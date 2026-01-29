import base64
import ctypes
import os
import json
import logging
import random
import re
import struct
import threading
import time
import uuid
from datetime import datetime
from contextlib import asynccontextmanager
import asyncio
import transformers
import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse, StreamingResponse
from fastapi.templating import Jinja2Templates
from collections import OrderedDict, defaultdict
from wasmtime import Engine, Linker, Module, Store


# -------------------------- 初始化 tokenizer --------------------------
chat_tokenizer_dir = "./"
try:
    tokenizer = transformers.AutoTokenizer.from_pretrained(
        chat_tokenizer_dir, trust_remote_code=True
    )
except:
    tokenizer = None
    logging.warning("Tokenizer加载失败，将使用简单估算")

# -------------------------- 日志配置 --------------------------
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s"
)
logger = logging.getLogger("main")

# -------------------------- 全局HTTP客户端配置 --------------------------
async_http_client = None

CLIENT_CONFIG = {
    "timeout": httpx.Timeout(30.0, connect=10.0),
    "limits": httpx.Limits(
        max_connections=100,
        max_keepalive_connections=20,
        keepalive_expiry=30.0
    ),
    "http2": True,
    "follow_redirects": True,
}


class PowSolver:
    """Manage a single WASM instance for DeepSeek PoW solving."""

    def __init__(self, wasm_path: str):
        self._wasm_path = wasm_path
        self._engine = Engine()
        self._lock = threading.Lock()
        self._store = None
        self._memory = None
        self._add_to_stack = None
        self._alloc = None
        self._wasm_solve = None
        self._memory_base = None

    def _initialize_instance(self):
        if self._memory is not None:
            return

        try:
            with open(self._wasm_path, "rb") as f:
                wasm_bytes = f.read()

            module = Module(self._engine, wasm_bytes)
            linker = Linker(self._engine)
            store = Store(self._engine)
            instance = linker.instantiate(store, module)
            exports = instance.exports(store)

            self._store = store
            self._memory = exports["memory"]
            self._add_to_stack = exports["__wbindgen_add_to_stack_pointer"]
            self._alloc = exports["__wbindgen_export_0"]
            self._wasm_solve = exports["wasm_solve"]
            self._memory_base = ctypes.cast(
                self._memory.data_ptr(self._store), ctypes.c_void_p
            ).value
        except Exception as exc:
            logger.error(f"[PowSolver] 初始化失败: {exc}")
            raise RuntimeError(
                f"加载 wasm 文件失败: {self._wasm_path}, 错误: {exc}"
            ) from exc

    def solve(
        self,
        algorithm: str,
        challenge_str: str,
        salt: str,
        difficulty: int,
        expire_at: int,
        signature: str,
        target_path: str,
    ) -> int | None:
        if algorithm != "DeepSeekHashV1":
            raise ValueError(f"不支持的算法：{algorithm}")

        prefix = f"{salt}_{expire_at}_"

        with self._lock:
            self._initialize_instance()

            store = self._store
            add_to_stack = self._add_to_stack
            alloc = self._alloc
            wasm_solve = self._wasm_solve
            ptr = self._memory.data_ptr(store)
            if ptr is None:
                raise RuntimeError("无法获取 WASM 内存基地址")
            base_addr = ctypes.cast(ptr, ctypes.c_void_p).value
            if base_addr != self._memory_base:
                self._memory_base = base_addr

            def write_memory(offset: int, data: bytes):
                ctypes.memmove(base_addr + offset, data, len(data))

            def read_memory(offset: int, size: int) -> bytes:
                return ctypes.string_at(base_addr + offset, size)

            def encode_string(text: str):
                data = text.encode("utf-8")
                length = len(data)
                ptr_val = alloc(store, length, 1)
                ptr = int(getattr(ptr_val, "value", ptr_val))
                write_memory(ptr, data)
                return ptr, length

            retptr = add_to_stack(store, -16)
            ptr_challenge, len_challenge = encode_string(challenge_str)
            ptr_prefix, len_prefix = encode_string(prefix)

            wasm_solve(
                store,
                retptr,
                ptr_challenge,
                len_challenge,
                ptr_prefix,
                len_prefix,
                float(difficulty),
            )

            status_bytes = read_memory(retptr, 4)
            if len(status_bytes) != 4:
                add_to_stack(store, 16)
                raise RuntimeError("读取状态字节失败")

            status = struct.unpack("<i", status_bytes)[0]
            value_bytes = read_memory(retptr + 8, 8)
            if len(value_bytes) != 8:
                add_to_stack(store, 16)
                raise RuntimeError("读取结果字节失败")

            add_to_stack(store, 16)

            if status == 0:
                return None

            value = struct.unpack("<d", value_bytes)[0]
            return int(value)

    async def solve_async(self, *args, **kwargs):
        return await asyncio.to_thread(self.solve, *args, **kwargs)

    async def warmup(self):
        await asyncio.to_thread(self._warmup_sync)

    def _warmup_sync(self):
        with self._lock:
            self._initialize_instance()


class PowResponseCache:
    """Lightweight in-memory cache for recent PoW responses."""

    def __init__(self, max_entries: int = 256):
        self._max_entries = max_entries
        self._entries = OrderedDict()
        self._lock = threading.Lock()

    @staticmethod
    def _make_key(challenge: dict):
        if not challenge:
            return None
        return (
            challenge.get("algorithm"),
            challenge.get("challenge"),
            challenge.get("salt"),
            challenge.get("signature"),
            challenge.get("target_path"),
        )

    def get(self, challenge: dict) -> str | None:
        key = self._make_key(challenge)
        if key is None:
            return None

        now = time.time()
        with self._lock:
            entry = self._entries.get(key)
            if not entry:
                return None

            if entry["expire_at"] <= now:
                self._entries.pop(key, None)
                return None

            self._entries.move_to_end(key)
            return entry["encoded"]

    def set(self, challenge: dict, encoded: str, expire_at: int | float):
        key = self._make_key(challenge)
        if key is None:
            return

        ttl = float(expire_at or 0) - 0.5
        if ttl <= time.time():
            return

        with self._lock:
            self._entries[key] = {"encoded": encoded, "expire_at": ttl}
            self._entries.move_to_end(key)
            if len(self._entries) > self._max_entries:
                self._entries.popitem(last=False)


class AccountPool:
    def __init__(self):
        self.accounts = []
        self.active_counts = defaultdict(int)
        self.lock = asyncio.Lock()
        self.max_accounts = 0
        self._last_empty_warning = 0.0

    async def initialize(self, config):
        """Load accounts from config and warm up pool."""
        configured_max = config.get("max_active_accounts")
        all_accounts = config.get("accounts", [])
        if isinstance(configured_max, int) and configured_max > 0:
            self.max_accounts = min(configured_max, len(all_accounts))
        else:
            self.max_accounts = len(all_accounts)

        self.accounts = []
        self.active_counts = defaultdict(int)
        self._last_empty_warning = 0.0

        if not all_accounts:
            logger.warning("配置中没有可用账户")
            return

        selected_accounts = random.sample(
            all_accounts,
            min(self.max_accounts, len(all_accounts))
        )

        logger.info(f"开始初始化 {len(selected_accounts)} 个账户...")

        tasks = [self._init_account(account) for account in selected_accounts]
        results = await asyncio.gather(*tasks, return_exceptions=True)

        success_count = sum(1 for result in results if result is True)
        logger.info(f"账户池初始化完成: {success_count}/{len(selected_accounts)} 个账户可用")

    async def _init_account(self, account):
        """Prepare a single account before adding it to the pool."""
        acc_id = get_account_identifier(account)

        try:
            if account.get("token", "").strip():
                logger.info(f"账户 {acc_id} 已有token，直接加入池")
                async with self.lock:
                    self.accounts.append(account)
                return True

            logger.info(f"账户 {acc_id} 尝试登录...")
            await login_deepseek_via_account(account)
            async with self.lock:
                self.accounts.append(account)
            logger.info(f"账户 {acc_id} 登录成功")
            return True

        except Exception as exc:
            logger.error(f"账户 {acc_id} 初始化失败: {exc}")
            return False

    async def acquire(self, exclude_ids=None):
        """Randomly pick an account, ignoring concurrency limits."""
        exclude_ids = set(exclude_ids or [])

        async with self.lock:
            if not self.accounts:
                now = time.time()
                if now - self._last_empty_warning > 30:
                    logger.warning("账户池中没有可用账户")
                    self._last_empty_warning = now
                return None

            candidates = [
                account for account in self.accounts
                if get_account_identifier(account) not in exclude_ids
            ]

            if not candidates:
                candidates = self.accounts

            account = random.choice(candidates)
            acc_id = get_account_identifier(account)
            self.active_counts[acc_id] += 1

            logger.debug(f"获取账户: {acc_id}, 当前活跃会话: {self.active_counts[acc_id]}")
            return account

    async def release(self, account):
        """Release an account reference after use."""
        if account is None:
            return

        acc_id = get_account_identifier(account)
        async with self.lock:
            if acc_id in self.active_counts:
                self.active_counts[acc_id] = max(0, self.active_counts[acc_id] - 1)
                if self.active_counts[acc_id] == 0:
                    del self.active_counts[acc_id]

            logger.debug(f"释放账户: {acc_id}, 当前活跃会话: {self.active_counts.get(acc_id, 0)}")

    async def get_status(self):
        """Expose current pool metrics."""
        async with self.lock:
            total_accounts = len(self.accounts)
            busy_ids = set(self.active_counts.keys())
            idle_accounts = total_accounts - len(busy_ids)
            active_sessions = sum(self.active_counts.values())
            return {
                "total": total_accounts,
                "available": idle_accounts,
                "in_use": len(busy_ids),
                "active_sessions": active_sessions,
                "max_accounts": self.max_accounts,
            }


account_pool = AccountPool()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """应用生命周期管理"""
    global async_http_client

    async_http_client = httpx.AsyncClient(**CLIENT_CONFIG)
    logger.info("HTTP客户端初始化完成")

    global CONFIG
    config = load_config()
    CONFIG = config
    await account_pool.initialize(config)
    try:
        await pow_solver.warmup()
        logger.info("PoW 求解器初始化完成")
    except Exception as exc:
        logger.warning(f"PoW 求解器预热失败: {exc}")

    yield

    await async_http_client.aclose()
    logger.info("HTTP客户端已关闭")


app = FastAPI(lifespan=lifespan)

# 添加 CORS 中间件
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["GET", "POST", "OPTIONS", "PUT", "DELETE"],
    allow_headers=["Content-Type", "Authorization", "X-OA-Key"],
)

# 模板目录
templates = Jinja2Templates(directory="templates")

# ----------------------------------------------------------------------
# (1) 配置文件的读写函数
# ----------------------------------------------------------------------
CONFIG_PATH = "config.json"


def load_config():
    """从 config.json 加载配置，出错则返回空 dict"""
    env_config = os.getenv("API_CONFIG", "").strip()
    if env_config:
        try:
            loaded = json.loads(env_config)
            if isinstance(loaded, dict):
                return loaded
            logger.warning(
                "[load_config] API_CONFIG 环境变量内容不是有效的 JSON 对象，改用文件配置"
            )
        except json.JSONDecodeError as exc:
            logger.warning(
                f"[load_config] 解析 API_CONFIG 环境变量失败: {exc}，改用文件配置"
            )
    try:
        with open(CONFIG_PATH, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception as e:
        logger.warning(f"[load_config] 无法读取配置文件: {e}")
        return {}


def save_config(cfg):
    """将配置写回 config.json"""
    try:
        with open(CONFIG_PATH, "w", encoding="utf-8") as f:
            json.dump(cfg, f, ensure_ascii=False, indent=2)
    except Exception as e:
        logger.error(f"[save_config] 写入 config.json 失败: {e}")


CONFIG = load_config()

# ----------------------------------------------------------------------
# (2) DeepSeek 相关常量
# ----------------------------------------------------------------------
DEEPSEEK_HOST = "chat.deepseek.com"
DEEPSEEK_LOGIN_URL = f"https://{DEEPSEEK_HOST}/api/v0/users/login"
DEEPSEEK_CREATE_SESSION_URL = f"https://{DEEPSEEK_HOST}/api/v0/chat_session/create"
DEEPSEEK_CREATE_POW_URL = f"https://{DEEPSEEK_HOST}/api/v0/chat/create_pow_challenge"
DEEPSEEK_COMPLETION_URL = f"https://{DEEPSEEK_HOST}/api/v0/chat/completion"
BASE_HEADERS = {
    "Host": "chat.deepseek.com",
    "User-Agent": "DeepSeek/1.0.13 Android/35",
    "Accept": "application/json",
    "Accept-Encoding": "gzip",
    "Content-Type": "application/json",
    "x-client-platform": "android",
    "x-client-version": "1.3.0-auto-resume",
    "x-client-locale": "zh_CN",
    "accept-charset": "UTF-8",
}

# ----------------------------------------------------------------------
# (2.1) Claude 相关常量
# ----------------------------------------------------------------------
CLAUDE_DEFAULT_MODEL = "claude-sonnet-4-20250514"

# WASM 模块文件路径
WASM_PATH = "sha3_wasm_bg.7b9ca65ddd.wasm"

pow_solver = PowSolver(WASM_PATH)
pow_cache = PowResponseCache()


# ----------------------------------------------------------------------
# 辅助函数：获取账号唯一标识
# ----------------------------------------------------------------------
def get_account_identifier(account):
    """返回账号的唯一标识，优先使用 email，否则使用 mobile"""
    return account.get("email", "").strip() or account.get("mobile", "").strip()


# ----------------------------------------------------------------------
# (3) 登录函数：支持使用 email 或 mobile 登录
# ----------------------------------------------------------------------
async def login_deepseek_via_account(account):
    """使用 account 中的 email 或 mobile 登录 DeepSeek (异步版本)"""
    email = account.get("email", "").strip()
    mobile = account.get("mobile", "").strip()
    password = account.get("password", "").strip()
    
    if not password or (not email and not mobile):
        raise HTTPException(
            status_code=400,
            detail="账号缺少必要的登录信息（必须提供 email 或 mobile 以及 password）",
        )
    
    if email:
        payload = {
            "email": email,
            "password": password,
            "device_id": "deepseek_to_api",
            "os": "android",
        }
    else:
        payload = {
            "mobile": mobile,
            "area_code": None,
            "password": password,
            "device_id": "deepseek_to_api",
            "os": "android",
        }
    
    try:
        resp = await async_http_client.post(DEEPSEEK_LOGIN_URL, headers=BASE_HEADERS, json=payload)
        resp.raise_for_status()
    except Exception as e:
        logger.error(f"[login_deepseek_via_account] 登录请求异常: {e}")
        raise HTTPException(status_code=500, detail="Account login failed: 请求异常")
    
    try:
        logger.warning(f"[login_deepseek_via_account] {resp.text}")
        data = resp.json()
    except Exception as e:
        logger.error(f"[login_deepseek_via_account] JSON解析失败: {e}")
        raise HTTPException(
            status_code=500, detail="Account login failed: invalid JSON response"
        )
    
    if (
        data.get("data") is None
        or data["data"].get("biz_data") is None
        or data["data"]["biz_data"].get("user") is None
    ):
        logger.error(f"[login_deepseek_via_account] 登录响应格式错误: {data}")
        raise HTTPException(
            status_code=500, detail="Account login failed: invalid response format"
        )
    
    new_token = data["data"]["biz_data"]["user"].get("token")
    if not new_token:
        logger.error(f"[login_deepseek_via_account] 登录响应中缺少 token: {data}")
        raise HTTPException(
            status_code=500, detail="Account login failed: missing token"
        )
    
    account["token"] = new_token
    save_config(CONFIG)
    return new_token


def generate_client_stream_id():
    """Create a DeepSeek-style client_stream_id"""
    date_part = datetime.utcnow().strftime("%Y%m%d")
    random_part = uuid.uuid4().hex[:16]
    return f"{date_part}-{random_part}"


async def switch_account(request):
    """Release current account and acquire a replacement from the pool."""
    if not getattr(request.state, "use_config_token", False):
        return False

    if not hasattr(request.state, "failed_accounts"):
        request.state.failed_accounts = set()

    current_account = getattr(request.state, "account", None)
    if current_account is not None:
        request.state.failed_accounts.add(get_account_identifier(current_account))
        await account_pool.release(current_account)

    new_account = await account_pool.acquire(request.state.failed_accounts)
    if not new_account:
        request.state.account = None
        request.state.deepseek_token = None
        return False

    request.state.account = new_account
    request.state.deepseek_token = new_account.get("token")
    return True


# ----------------------------------------------------------------------
# (5) 判断调用模式
# ----------------------------------------------------------------------
async def determine_mode_and_token(request: Request):
    """根据请求头 X-OA-Key 或 Authorization Bearer 判断使用模式 (异步版本)"""
    caller_key = request.headers.get("X-OA-Key", "").strip()
    if not caller_key:
        auth_header = request.headers.get("Authorization", "").strip()
        if auth_header.lower().startswith("bearer "):
            caller_key = auth_header[7:].strip()

    if not caller_key:
        raise HTTPException(
            status_code=401,
            detail="Unauthorized: missing X-OA-Key or Authorization Bearer header.",
        )

    config_keys = CONFIG.get("keys", [])
    
    if caller_key in config_keys:
        request.state.use_config_token = True
        request.state.failed_accounts = set()

        selected_account = await account_pool.acquire()

        if not selected_account:
            raise HTTPException(
                status_code=429,
                detail="No accounts available in pool.",
            )

        if not selected_account.get("token", "").strip():
            try:
                await login_deepseek_via_account(selected_account)
            except Exception as exc:
                logger.error(
                    f"[determine_mode_and_token] 账号 {get_account_identifier(selected_account)} 登录失败：{exc}"
                )
                raise HTTPException(status_code=500, detail="Account login failed.")

        request.state.deepseek_token = selected_account.get("token")
        request.state.account = selected_account
    else:
        request.state.use_config_token = False
        request.state.deepseek_token = caller_key


def get_auth_headers(request: Request):
    """返回 DeepSeek 请求所需的公共请求头"""
    return {**BASE_HEADERS, "authorization": f"Bearer {request.state.deepseek_token}"}


# ----------------------------------------------------------------------
# Claude 认证相关函数
# ----------------------------------------------------------------------
async def determine_claude_mode_and_token(request: Request):
    """Claude认证"""
    await determine_mode_and_token(request)


# ----------------------------------------------------------------------
# 新增：账户池状态查询路由
# ----------------------------------------------------------------------
@app.get("/pool/status")
async def pool_status():
    """查询账户池状态"""
    status = await account_pool.get_status()
    return JSONResponse(content=status, status_code=200)


# ----------------------------------------------------------------------
# OpenAI到Claude格式转换函数
# ----------------------------------------------------------------------
def convert_claude_to_deepseek(claude_request):
    """将Claude格式的请求转换为DeepSeek格式"""
    messages = claude_request.get("messages", [])
    model = claude_request.get("model", CLAUDE_DEFAULT_MODEL)
    
    claude_mapping = CONFIG.get("claude_model_mapping", {
        "fast": "deepseek-chat",
        "slow": "deepseek-chat"
    })
    
    if "opus" in model.lower() or "reasoner" in model.lower() or "slow" in model.lower():
        deepseek_model = claude_mapping.get("slow", "deepseek-chat")
    else:
        deepseek_model = claude_mapping.get("fast", "deepseek-chat")
    
    deepseek_request = {
        "model": deepseek_model,
        "messages": messages.copy()
    }
    
    if "system" in claude_request:
        system_msg = {"role": "system", "content": claude_request["system"]}
        deepseek_request["messages"].insert(0, system_msg)
    
    if "temperature" in claude_request:
        deepseek_request["temperature"] = claude_request["temperature"]
    if "top_p" in claude_request:
        deepseek_request["top_p"] = claude_request["top_p"]
    if "stop_sequences" in claude_request:
        deepseek_request["stop"] = claude_request["stop_sequences"]
    if "stream" in claude_request:
        deepseek_request["stream"] = claude_request["stream"]
        
    return deepseek_request


def convert_deepseek_to_claude_format(deepseek_response, original_claude_model=CLAUDE_DEFAULT_MODEL):
    """将DeepSeek响应转换为Claude格式"""
    if isinstance(deepseek_response, dict):
        claude_response = deepseek_response.copy()
        claude_response["model"] = original_claude_model
        return claude_response
    
    return deepseek_response


# ----------------------------------------------------------------------
# (7) 创建会话
# ----------------------------------------------------------------------
async def create_session(request: Request, max_attempts=3):
    """创建会话 (异步版本)"""
    attempts = 0
    while attempts < max_attempts:
        headers = get_auth_headers(request)
        try:
            resp = await async_http_client.post(
                DEEPSEEK_CREATE_SESSION_URL, headers=headers, json={"agent": "chat"}
            )
        except Exception as e:
            logger.error(f"[create_session] 请求异常: {e}")
            attempts += 1
            await asyncio.sleep(1)
            continue
        
        try:
            logger.debug("[create_session] 响应: %s", resp.text[:256])
            data = resp.json()
        except Exception as e:
            logger.error(f"[create_session] JSON解析异常: {e}")
            data = {}
        
        if resp.status_code == 200 and data.get("code") == 0:
            session_id = data["data"]["biz_data"]["id"]
            return session_id
        else:
            code = data.get("code")
            logger.warning(
                f"[create_session] 创建会话失败, code={code}, msg={data.get('msg')}"
            )
            
            if request.state.use_config_token:
                rotated = await switch_account(request)
                if rotated:
                    attempts += 1
                    await asyncio.sleep(1)
                    continue
                break
            else:
                attempts += 1
                await asyncio.sleep(1)
                continue
        
        attempts += 1
    
    return None


# ----------------------------------------------------------------------
# (7.2) 获取 PoW 响应
# ----------------------------------------------------------------------
async def get_pow_response(request: Request, max_attempts=3):
    """获取 PoW 响应 (异步版本)"""
    attempts = 0
    while attempts < max_attempts:
        headers = get_auth_headers(request)
        try:
            resp = await async_http_client.post(
                DEEPSEEK_CREATE_POW_URL,
                headers=headers,
                json={"target_path": "/api/v0/chat/completion"},
            )
        except Exception as e:
            logger.error(f"[get_pow_response] 请求异常: {e}")
            attempts += 1
            await asyncio.sleep(1)
            continue
        
        try:
            data = resp.json()
        except Exception as e:
            logger.error(f"[get_pow_response] JSON解析异常: {e}")
            data = {}
        
        if resp.status_code == 200 and data.get("code") == 0:
            challenge = data["data"]["biz_data"]["challenge"]
            difficulty = challenge.get("difficulty", 144000)
            expire_at = challenge.get("expire_at", 1680000000)

            cached = pow_cache.get(challenge)
            if cached is not None:
                return cached
            
            try:
                answer = await pow_solver.solve_async(
                    challenge["algorithm"],
                    challenge["challenge"],
                    challenge["salt"],
                    difficulty,
                    expire_at,
                    challenge.get("signature", ""),
                    challenge.get("target_path", ""),
                )
            except Exception as e:
                logger.error(f"[get_pow_response] PoW 答案计算异常: {e}")
                answer = None
            
            if answer is None:
                logger.warning("[get_pow_response] PoW 答案计算失败，重试中...")
                attempts += 1
                await asyncio.sleep(1)
                continue
            
            pow_dict = {
                "algorithm": challenge["algorithm"],
                "challenge": challenge["challenge"],
                "salt": challenge["salt"],
                "answer": answer,
                "signature": challenge["signature"],
                "target_path": challenge["target_path"],
            }
            pow_str = json.dumps(pow_dict, separators=(",", ":"), ensure_ascii=False)
            encoded = base64.b64encode(pow_str.encode("utf-8")).decode("utf-8").rstrip()
            pow_cache.set(challenge, encoded, expire_at)
            return encoded
        else:
            code = data.get("code")
            logger.warning(
                f"[get_pow_response] 获取 PoW 失败, code={code}, msg={data.get('msg')}"
            )
            
            if request.state.use_config_token:
                rotated = await switch_account(request)
                if rotated:
                    attempts += 1
                    await asyncio.sleep(1)
                    continue
                break
            else:
                attempts += 1
                await asyncio.sleep(1)
                continue
            
            attempts += 1
    
    return None


# ----------------------------------------------------------------------
# (8) 路由：/v1/models
# ----------------------------------------------------------------------
@app.get("/v1/models")
async def list_models():
    models_list = [
        {
            "id": "deepseek-chat",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
        {
            "id": "deepseek-reasoner",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
        {
            "id": "deepseek-chat-search",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
        {
            "id": "deepseek-reasoner-search",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
    ]
    data = {"object": "list", "data": models_list}
    return JSONResponse(content=data, status_code=200)


# ----------------------------------------------------------------------
# Claude 路由：模型列表
# ----------------------------------------------------------------------
@app.get("/anthropic/v1/models")
async def list_claude_models():
    models_list = [
        {
            "id": "claude-sonnet-4-20250514",
            "object": "model",
            "created": 1715635200,
            "owned_by": "anthropic",
        },
        {
            "id": "claude-sonnet-4-20250514-fast",
            "object": "model",
            "created": 1715635200,
            "owned_by": "anthropic",
        },
        {
            "id": "claude-sonnet-4-20250514-slow",
            "object": "model",
            "created": 1715635200,
            "owned_by": "anthropic",
        },
    ]
    data = {"object": "list", "data": models_list}
    return JSONResponse(content=data, status_code=200)


# ----------------------------------------------------------------------
# 消息预处理函数
# ----------------------------------------------------------------------
def messages_prepare(messages: list) -> str:
    """处理消息列表，合并连续相同角色的消息"""
    processed = []
    for m in messages:
        role = m.get("role", "")
        content = m.get("content", "")
        if isinstance(content, list):
            texts = [
                item.get("text", "") for item in content if item.get("type") == "text"
            ]
            text = "\n".join(texts)
        else:
            text = str(content)
        processed.append({"role": role, "text": text})
    
    if not processed:
        return ""
    
    merged = [processed[0]]
    for msg in processed[1:]:
        if msg["role"] == merged[-1]["role"]:
            merged[-1]["text"] += "\n\n" + msg["text"]
        else:
            merged.append(msg)
    
    parts = []
    for idx, block in enumerate(merged):
        role = block["role"]
        text = block["text"]
        if role == "assistant":
            parts.append(f"<｜Assistant｜>{text}<｜end▁of▁sentence｜>")
        elif role in ("user", "system"):
            if idx > 0:
                parts.append(f"<｜User｜>{text}")
            else:
                parts.append(text)
        else:
            parts.append(text)
    
    final_prompt = "".join(parts)
    final_prompt = re.sub(r"!\[(.*?)\]\((.*?)\)", r"[\1](\2)", final_prompt)
    return final_prompt


KEEP_ALIVE_TIMEOUT = 5


# ----------------------------------------------------------------------
# (10) 路由：/v1/chat/completions
# ----------------------------------------------------------------------
@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    try:
        try:
            await determine_mode_and_token(request)
        except HTTPException as exc:
            return JSONResponse(
                status_code=exc.status_code, content={"error": exc.detail}
            )
        except Exception as exc:
            logger.error(f"[chat_completions] determine_mode_and_token 异常: {exc}")
            return JSONResponse(
                status_code=500, content={"error": "Account login failed."}
            )

        req_data = await request.json()
        model = req_data.get("model")
        messages = req_data.get("messages", [])
        
        if not model or not messages:
            raise HTTPException(
                status_code=400, detail="Request must include 'model' and 'messages'."
            )
        
        model_lower = model.lower()
        if model_lower in ["deepseek-v3", "deepseek-chat"]:
            thinking_enabled = False
            search_enabled = False
        elif model_lower in ["deepseek-r1", "deepseek-reasoner"]:
            thinking_enabled = True
            search_enabled = False
        elif model_lower in ["deepseek-v3-search", "deepseek-chat-search"]:
            thinking_enabled = False
            search_enabled = True
        elif model_lower in ["deepseek-r1-search", "deepseek-reasoner-search"]:
            thinking_enabled = True
            search_enabled = True
        else:
            raise HTTPException(
                status_code=503, detail=f"Model '{model}' is not available."
            )
        
        final_prompt = messages_prepare(messages)
        
        session_id = await create_session(request)
        if not session_id:
            raise HTTPException(status_code=401, detail="invalid token.")
        
        pow_resp = await get_pow_response(request)
        if not pow_resp:
            raise HTTPException(
                status_code=401,
                detail="Failed to get PoW (invalid token or unknown error).",
            )
        
        headers = {**get_auth_headers(request), "x-ds-pow-response": pow_resp}
        payload = {
            "chat_session_id": session_id,
            "parent_message_id": None,
            "client_stream_id": generate_client_stream_id(),
            "prompt": final_prompt,
            "ref_file_ids": [],
            "thinking_enabled": thinking_enabled,
            "search_enabled": search_enabled,
        }

        created_time = int(time.time())
        completion_id = f"{session_id}"

        if bool(req_data.get("stream", False)):
            async def sse_stream():
                deepseek_resp = None
                try:
                    async with async_http_client.stream(
                        "POST",
                        DEEPSEEK_COMPLETION_URL,
                        headers=headers,
                        json=payload,
                        timeout=120.0
                    ) as deepseek_resp:
                        if deepseek_resp.status_code != 200:
                            yield f"data: {json.dumps({'error': 'Failed to get completion'})}\n\n"
                            return

                        final_text = ""
                        final_thinking = ""
                        first_chunk_sent = False
                        last_send_time = time.time()
                        ptype = "text"

                        async for raw_line in deepseek_resp.aiter_lines():
                            try:
                                line = raw_line
                            except Exception as e:
                                logger.warning(f"[sse_stream] 解码失败: {e}")
                                continue

                            if not line:
                                continue

                            if line.startswith("data:"):
                                data_str = line[5:].strip()
                                if data_str == "[DONE]":
                                    break

                                try:
                                    chunk = json.loads(data_str)

                                    if "v" in chunk:
                                        v_value = chunk["v"]
                                        content = None

                                        if "p" in chunk and chunk.get("p") == "response/search_status":
                                            continue

                                        if "p" in chunk and chunk.get("p") == "response/thinking_content":
                                            ptype = "thinking"
                                        elif "p" in chunk and chunk.get("p") == "response/content":
                                            ptype = "text"

                                        if isinstance(v_value, str):
                                            content = v_value
                                        elif isinstance(v_value, list):
                                            for item in v_value:
                                                if item.get("p") == "status" and item.get("v") == "FINISHED":
                                                    prompt_tokens = len(final_prompt) // 4
                                                    thinking_tokens = len(final_thinking) // 4
                                                    completion_tokens = len(final_text) // 4
                                                    usage = {
                                                        "prompt_tokens": prompt_tokens,
                                                        "completion_tokens": thinking_tokens + completion_tokens,
                                                        "total_tokens": prompt_tokens + thinking_tokens + completion_tokens,
                                                        "completion_tokens_details": {
                                                            "reasoning_tokens": thinking_tokens
                                                        },
                                                    }
                                                    finish_chunk = {
                                                        "id": completion_id,
                                                        "object": "chat.completion.chunk",
                                                        "created": created_time,
                                                        "model": model,
                                                        "choices": [
                                                            {
                                                                "delta": {},
                                                                "index": 0,
                                                                "finish_reason": "stop",
                                                            }
                                                        ],
                                                        "usage": usage,
                                                    }
                                                    yield f"data: {json.dumps(finish_chunk, ensure_ascii=False)}\n\n"
                                                    yield "data: [DONE]\n\n"
                                                    return
                                            continue
                                        else:
                                            continue

                                        if content is None:
                                            continue

                                        if search_enabled and content.startswith("[citation:"):
                                            content = ""

                                        if ptype == "thinking":
                                            if thinking_enabled:
                                                final_thinking += content
                                        elif ptype == "text":
                                            final_text += content

                                        delta_obj = {}
                                        if not first_chunk_sent:
                                            delta_obj["role"] = "assistant"
                                            first_chunk_sent = True

                                        if ptype == "thinking":
                                            if thinking_enabled:
                                                delta_obj["reasoning_content"] = content
                                        elif ptype == "text":
                                            delta_obj["content"] = content

                                        if delta_obj:
                                            out_chunk = {
                                                "id": completion_id,
                                                "object": "chat.completion.chunk",
                                                "created": created_time,
                                                "model": model,
                                                "choices": [{
                                                    "delta": delta_obj,
                                                    "index": 0,
                                                }],
                                            }
                                            yield f"data: {json.dumps(out_chunk, ensure_ascii=False)}\n\n"
                                            last_send_time = time.time()

                                except Exception as e:
                                    logger.warning(f"[sse_stream] 无法解析: {data_str}, 错误: {e}")
                                    continue

                except Exception as e:
                    logger.error(f"[sse_stream] 异常: {e}")
                finally:
                    if getattr(request.state, "use_config_token", False):
                        await account_pool.release(getattr(request.state, "account", None))
                        request.state.account = None
                        request.state.account_released = True

            return StreamingResponse(
                sse_stream(),
                media_type="text/event-stream",
                headers={"Content-Type": "text/event-stream"},
            )
        else:
            # 非流式响应
            async with async_http_client.stream(
                "POST",
                DEEPSEEK_COMPLETION_URL,
                headers=headers,
                json=payload,
                timeout=120.0
            ) as deepseek_resp:
                think_list = []
                text_list = []
                result = None
                ptype = "text"

                async for raw_line in deepseek_resp.aiter_lines():
                    try:
                        line = raw_line
                    except Exception as e:
                        logger.warning(f"[chat_completions] 解码失败: {e}")
                        continue

                    if not line:
                        continue

                    if line.startswith("data:"):
                        data_str = line[5:].strip()
                        if data_str == "[DONE]":
                            break

                        try:
                            chunk = json.loads(data_str)

                            if "v" in chunk:
                                v_value = chunk["v"]

                                if "p" in chunk and chunk.get("p") == "response/search_status":
                                    continue

                                if "p" in chunk and chunk.get("p") == "response/thinking_content":
                                    ptype = "thinking"
                                elif "p" in chunk and chunk.get("p") == "response/content":
                                    ptype = "text"

                                if isinstance(v_value, str):
                                    if search_enabled and v_value.startswith("[citation:"):
                                        continue
                                    if ptype == "thinking":
                                        think_list.append(v_value)
                                    else:
                                        text_list.append(v_value)

                                elif isinstance(v_value, list):
                                    for item in v_value:
                                        if item.get("p") == "status" and item.get("v") == "FINISHED":
                                            final_reasoning = "".join(think_list)
                                            final_content = "".join(text_list)
                                            prompt_tokens = len(final_prompt) // 4
                                            reasoning_tokens = len(final_reasoning) // 4
                                            completion_tokens = len(final_content) // 4
                                            result = {
                                                "id": completion_id,
                                                "object": "chat.completion",
                                                "created": created_time,
                                                "model": model,
                                                "choices": [
                                                    {
                                                        "index": 0,
                                                        "message": {
                                                            "role": "assistant",
                                                            "content": final_content,
                                                            "reasoning_content": final_reasoning,
                                                        },
                                                        "finish_reason": "stop",
                                                    }
                                                ],
                                                "usage": {
                                                    "prompt_tokens": prompt_tokens,
                                                    "completion_tokens": reasoning_tokens + completion_tokens,
                                                    "total_tokens": prompt_tokens + reasoning_tokens + completion_tokens,
                                                    "completion_tokens_details": {
                                                        "reasoning_tokens": reasoning_tokens
                                                    },
                                                },
                                            }
                                            return JSONResponse(content=result, status_code=200)

                        except Exception as e:
                            logger.warning(f"[chat_completions] 无法解析: {data_str}, 错误: {e}")
                            continue

                # 如果没有收到FINISHED状态，构造默认结果
                final_content = "".join(text_list)
                final_reasoning = "".join(think_list)
                prompt_tokens = len(final_prompt) // 4
                reasoning_tokens = len(final_reasoning) // 4
                completion_tokens = len(final_content) // 4
                result = {
                    "id": completion_id,
                    "object": "chat.completion",
                    "created": created_time,
                    "model": model,
                    "choices": [
                        {
                            "index": 0,
                            "message": {
                                "role": "assistant",
                                "content": final_content,
                                "reasoning_content": final_reasoning,
                            },
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": {
                        "prompt_tokens": prompt_tokens,
                        "completion_tokens": reasoning_tokens + completion_tokens,
                        "total_tokens": prompt_tokens + reasoning_tokens + completion_tokens,
                    },
                }
                return JSONResponse(content=result, status_code=200)

    except HTTPException as exc:
        return JSONResponse(status_code=exc.status_code, content={"error": exc.detail})
    except Exception as exc:
        logger.error(f"[chat_completions] 未知异常: {exc}")
        return JSONResponse(status_code=500, content={"error": "Internal Server Error"})
    finally:
        if getattr(request.state, "use_config_token", False):
            await account_pool.release(getattr(request.state, "account", None))
            request.state.account = None
            request.state.account_released = True


# ----------------------------------------------------------------------
# Claude 路由：/anthropic/v1/messages
# ----------------------------------------------------------------------
@app.post("/anthropic/v1/messages")
async def claude_messages(request: Request):
    try:
        try:
            await determine_claude_mode_and_token(request)
        except HTTPException as exc:
            return JSONResponse(
                status_code=exc.status_code, content={"error": exc.detail}
            )
        except Exception as exc:
            logger.error(f"[claude_messages] determine_claude_mode_and_token 异常: {exc}")
            return JSONResponse(
                status_code=500, content={"error": "Claude authentication failed."}
            )

        req_data = await request.json()
        model = req_data.get("model")
        messages = req_data.get("messages", [])
        
        if not model or not messages:
            raise HTTPException(
                status_code=400, detail="Request must include 'model' and 'messages'."
            )
        
        # 标准化消息内容
        normalized_messages = []
        for message in messages:
            normalized_message = message.copy()
            if isinstance(message.get("content"), list):
                content_parts = []
                for content_block in message["content"]:
                    if content_block.get("type") == "text" and "text" in content_block:
                        content_parts.append(content_block["text"])
                    elif content_block.get("type") == "tool_result":
                        if "content" in content_block:
                            content_parts.append(str(content_block["content"]))
                if content_parts:
                    normalized_message["content"] = "\n".join(content_parts)
                elif isinstance(message.get("content"), list) and message["content"]:
                    normalized_message["content"] = message["content"]
                else:
                    normalized_message["content"] = ""
            normalized_messages.append(normalized_message)
        
        tools_requested = req_data.get("tools") or []
        has_tools = len(tools_requested) > 0
        
        payload = req_data.copy()
        payload["messages"] = normalized_messages.copy()
        
        if has_tools and not any(m.get("role") == "system" for m in payload["messages"]):
            tool_schemas = []
            for tool in tools_requested:
                tool_name = tool.get('name', 'unknown')
                tool_desc = tool.get('description', 'No description available')
                schema = tool.get('input_schema', {})
                
                tool_info = f"Tool: {tool_name}\nDescription: {tool_desc}"
                if 'properties' in schema:
                    props = []
                    required = schema.get('required', [])
                    for prop_name, prop_info in schema['properties'].items():
                        prop_type = prop_info.get('type', 'string')
                        is_req = ' (required)' if prop_name in required else ''
                        props.append(f"  - {prop_name}: {prop_type}{is_req}")
                    if props:
                        tool_info += f"\nParameters:\n{chr(10).join(props)}"
                tool_schemas.append(tool_info)
            
            system_message = {
                "role": "system",
                "content": f"""You are Claude, a helpful AI assistant. You have access to these tools:

{chr(10).join(tool_schemas)}

When you need to use tools, output ONLY valid JSON in this format:
{{"tool_calls": [{{"name": "tool_name", "input": {{"param": "value"}}}}]}}

You can call multiple tools in ONE response by including them in the same tool_calls array.
Do not include any text outside the JSON structure."""
            }
            payload["messages"].insert(0, system_message)

        # 转换为DeepSeek格式并调用
        deepseek_payload = convert_claude_to_deepseek(payload)
        
        model_lower = deepseek_payload["model"].lower()
        if model_lower in ["deepseek-v3", "deepseek-chat"]:
            thinking_enabled = False
            search_enabled = False
        elif model_lower in ["deepseek-r1", "deepseek-reasoner"]:
            thinking_enabled = True
            search_enabled = False
        elif model_lower in ["deepseek-v3-search", "deepseek-chat-search"]:
            thinking_enabled = False
            search_enabled = True
        elif model_lower in ["deepseek-r1-search", "deepseek-reasoner-search"]:
            thinking_enabled = True
            search_enabled = True
        else:
            thinking_enabled = False
            search_enabled = False
        
        final_prompt = messages_prepare(deepseek_payload["messages"])
        
        session_id = await create_session(request)
        if not session_id:
            raise HTTPException(status_code=401, detail="invalid token.")
        
        pow_resp = await get_pow_response(request)
        if not pow_resp:
            raise HTTPException(
                status_code=401,
                detail="Failed to get PoW.",
            )
        
        headers = {**get_auth_headers(request), "x-ds-pow-response": pow_resp}
        payload_ds = {
            "chat_session_id": session_id,
            "parent_message_id": None,
            "client_stream_id": generate_client_stream_id(),
            "prompt": final_prompt,
            "ref_file_ids": [],
            "thinking_enabled": thinking_enabled,
            "search_enabled": search_enabled,
        }

        created_time = int(time.time())
        
        if bool(req_data.get("stream", False)):
            async def claude_sse_stream():
                try:
                    async with async_http_client.stream(
                        "POST",
                        DEEPSEEK_COMPLETION_URL,
                        headers=headers,
                        json=payload_ds,
                        timeout=120.0
                    ) as deepseek_resp:
                        message_id = f"msg_{int(time.time())}_{random.randint(1000, 9999)}"
                        input_tokens = sum(len(str(m.get("content", ""))) for m in messages) // 4
                        output_tokens = 0
                        
                        full_response_text = ""
                        response_completed = False
                        
                        async for line in deepseek_resp.aiter_lines():
                            if not line:
                                continue
                            
                            line_str = line
                            if line_str.startswith('data:'):
                                data_str = line_str[5:].strip()
                                if data_str == '[DONE]':
                                    response_completed = True
                                    break
                                    
                                try:
                                    chunk = json.loads(data_str)
                                    if "v" in chunk and isinstance(chunk["v"], str):
                                        full_response_text += chunk["v"]
                                    elif "v" in chunk and isinstance(chunk["v"], list):
                                        for item in chunk["v"]:
                                            if item.get("p") == "status" and item.get("v") == "FINISHED":
                                                response_completed = True
                                                break
                                except (json.JSONDecodeError, KeyError):
                                    continue
                        
                        # 发送Claude格式事件
                        message_start = {
                            "type": "message_start",
                            "message": {
                                "id": message_id,
                                "type": "message",
                                "role": "assistant",
                                "model": model,
                                "content": [],
                                "stop_reason": None,
                                "stop_sequence": None,
                                "usage": {"input_tokens": input_tokens, "output_tokens": 0}
                            }
                        }
                        yield f"data: {json.dumps(message_start)}\n\n"
                        
                        # 检测工具调用
                        cleaned_response = full_response_text.strip()
                        detected_tools = []
                        tool_detected = False
                        
                        if cleaned_response.startswith('{"tool_calls":') and cleaned_response.endswith(']}'):
                            try:
                                tool_data = json.loads(cleaned_response)
                                for tool_call in tool_data.get('tool_calls', []):
                                    tool_name = tool_call.get('name')
                                    tool_input = tool_call.get('input', {})
                                    
                                    if any(tool.get('name') == tool_name for tool in tools_requested):
                                        detected_tools.append({
                                            'name': tool_name,
                                            'input': tool_input
                                        })
                                        tool_detected = True
                            except json.JSONDecodeError:
                                pass
                        
                        content_index = 0
                        if detected_tools:
                            stop_reason = "tool_use"
                            for tool_info in detected_tools:
                                tool_use_id = f"toolu_{int(time.time())}_{random.randint(1000, 9999)}_{content_index}"
                                tool_name = tool_info['name']
                                tool_input = tool_info['input']
                                
                                yield f"data: {json.dumps({'type': 'content_block_start', 'index': content_index, 'content_block': {'type': 'tool_use', 'id': tool_use_id, 'name': tool_name, 'input': tool_input}})}\n\n"
                                yield f"data: {json.dumps({'type': 'content_block_stop', 'index': content_index})}\n\n"

                                content_index += 1
                                output_tokens += len(str(tool_input)) // 4
                        else:
                            stop_reason = "end_turn"
                            if full_response_text:
                                yield f"data: {json.dumps({'type': 'content_block_start', 'index': 0, 'content_block': {'type': 'text', 'text': ''}})}\n\n"
                                yield f"data: {json.dumps({'type': 'content_block_delta', 'index': 0, 'delta': {'type': 'text_delta', 'text': full_response_text}})}\n\n"
                                yield f"data: {json.dumps({'type': 'content_block_stop', 'index': 0})}\n\n"
                                output_tokens += len(full_response_text) // 4

                        yield f"data: {json.dumps({'type': 'message_delta', 'delta': {'stop_reason': stop_reason, 'stop_sequence': None}, 'usage': {'output_tokens': output_tokens}})}\n\n"
                        yield f"data: {json.dumps({'type': 'message_stop'})}\n\n"
                            
                except Exception as e:
                    logger.error(f"[claude_sse_stream] 异常: {e}")
                    error_event = {
                        "type": "error",
                        "error": {"type": "api_error", "message": f"Stream processing error: {str(e)}"}
                    }
                    yield f"data: {json.dumps(error_event)}\n\n"
                finally:
                    if getattr(request.state, "use_config_token", False):
                        await account_pool.release(getattr(request.state, "account", None))
                        request.state.account = None
                        request.state.account_released = True

            return StreamingResponse(
                claude_sse_stream(),
                media_type="text/event-stream",
                headers={"Content-Type": "text/event-stream"},
            )
        else:
            # 非流式响应
            async with async_http_client.stream(
                "POST",
                DEEPSEEK_COMPLETION_URL,
                headers=headers,
                json=payload_ds,
                timeout=120.0
            ) as deepseek_resp:
                final_content = ""
                final_reasoning = ""
                
                async for line in deepseek_resp.aiter_lines():
                    if not line:
                        continue
                    
                    line_str = line
                    if line_str.startswith('data:'):
                        data_str = line_str[5:].strip()
                        if data_str == '[DONE]':
                            break
                        
                        try:
                            chunk = json.loads(data_str)
                            
                            if "v" in chunk:
                                v_value = chunk["v"]
                                
                                if "p" in chunk and chunk.get("p") == "response/search_status":
                                    continue
                                    
                                ptype = "text"
                                if "p" in chunk and chunk.get("p") == "response/thinking_content":
                                    ptype = "thinking"
                                elif "p" in chunk and chunk.get("p") == "response/content":
                                    ptype = "text"
                                
                                if isinstance(v_value, str):
                                    if ptype == "thinking":
                                        final_reasoning += v_value
                                    else:
                                        final_content += v_value
                                        
                        except json.JSONDecodeError:
                            continue
                
                # 检测工具调用
                cleaned_content = final_content.strip()
                detected_tools = []
                tool_detected = False
                
                if cleaned_content.startswith('{"tool_calls":') and cleaned_content.endswith(']}'):
                    try:
                        tool_data = json.loads(cleaned_content)
                        for tool_call in tool_data.get('tool_calls', []):
                            tool_name = tool_call.get('name')
                            tool_input = tool_call.get('input', {})
                            
                            if any(tool.get('name') == tool_name for tool in tools_requested):
                                detected_tools.append({
                                    'name': tool_name,
                                    'input': tool_input
                                })
                                tool_detected = True
                    except json.JSONDecodeError:
                        pass
                
                # 构造Claude响应
                claude_response = {
                    "id": f"msg_{int(time.time())}_{random.randint(1000, 9999)}",
                    "type": "message",
                    "role": "assistant",
                    "model": model,
                    "content": [],
                    "stop_reason": "tool_use" if detected_tools else "end_turn",
                    "stop_sequence": None,
                    "usage": {
                        "input_tokens": len(str(normalized_messages)) // 4,
                        "output_tokens": (len(final_content) + len(final_reasoning)) // 4
                    }
                }
                
                if final_reasoning:
                    claude_response["content"].append({
                        "type": "thinking",
                        "thinking": final_reasoning
                    })
                
                if detected_tools:
                    for i, tool_info in enumerate(detected_tools):
                        tool_use_id = f"toolu_{int(time.time())}_{random.randint(1000, 9999)}_{i}"
                        tool_name = tool_info['name']
                        tool_input = tool_info['input']
                        
                        claude_response["content"].append({
                            "type": "tool_use",
                            "id": tool_use_id,
                            "name": tool_name,
                            "input": tool_input
                        })
                else:
                    if final_content or not final_reasoning:
                        claude_response["content"].append({
                            "type": "text",
                            "text": final_content or "抱歉，没有生成有效的响应内容。"
                        })
                
                return JSONResponse(content=claude_response, status_code=200)

    except HTTPException as exc:
        return JSONResponse(status_code=exc.status_code, content={"error": {"type": "invalid_request_error", "message": exc.detail}})
    except Exception as exc:
        logger.error(f"[claude_messages] 未知异常: {exc}")
        return JSONResponse(status_code=500, content={"error": {"type": "api_error", "message": "Internal Server Error"}})
    finally:
        if getattr(request.state, "use_config_token", False):
            await account_pool.release(getattr(request.state, "account", None))


# ----------------------------------------------------------------------
# Claude 路由：/anthropic/v1/messages/count_tokens
# ----------------------------------------------------------------------
@app.post("/anthropic/v1/messages/count_tokens")
async def claude_count_tokens(request: Request):
    try:
        try:
            await determine_claude_mode_and_token(request)
        except HTTPException as exc:
            return JSONResponse(
                status_code=exc.status_code, content={"error": exc.detail}
            )

        req_data = await request.json()
        model = req_data.get("model")
        messages = req_data.get("messages", [])
        system = req_data.get("system", "")
        
        if not model or not messages:
            raise HTTPException(
                status_code=400, detail="Request must include 'model' and 'messages'."
            )
        
        def estimate_tokens(text):
            if isinstance(text, str):
                return len(text) // 4
            elif isinstance(text, list):
                return sum(estimate_tokens(item.get("text", "")) if isinstance(item, dict) else estimate_tokens(str(item)) for item in text)
            else:
                return len(str(text)) // 4
        
        input_tokens = 0
        
        if system:
            input_tokens += estimate_tokens(system)
            
        for message in messages:
            input_tokens += 2
            content = message.get("content", "")
            
            if isinstance(content, list):
                for content_block in content:
                    if isinstance(content_block, dict):
                        if content_block.get("type") == "text":
                            input_tokens += estimate_tokens(content_block.get("text", ""))
                        elif content_block.get("type") == "tool_result":
                            input_tokens += estimate_tokens(content_block.get("content", ""))
                        else:
                            input_tokens += estimate_tokens(str(content_block))
                    else:
                        input_tokens += estimate_tokens(str(content_block))
            else:
                input_tokens += estimate_tokens(content)
        
        tools = req_data.get("tools", [])
        if tools:
            for tool in tools:
                input_tokens += estimate_tokens(tool.get("name", ""))
                input_tokens += estimate_tokens(tool.get("description", ""))
                input_schema = tool.get("input_schema", {})
                input_tokens += estimate_tokens(json.dumps(input_schema, ensure_ascii=False))
        
        response = {
            "input_tokens": max(1, input_tokens)
        }
        
        return JSONResponse(content=response, status_code=200)
        
    except HTTPException as exc:
        return JSONResponse(status_code=exc.status_code, content={"error": {"type": "invalid_request_error", "message": exc.detail}})
    except Exception as exc:
        logger.error(f"[claude_count_tokens] 未知异常: {exc}")
        return JSONResponse(status_code=500, content={"error": {"type": "api_error", "message": "Internal Server Error"}})
    finally:
        if getattr(request.state, "use_config_token", False):
            await account_pool.release(getattr(request.state, "account", None))
            request.state.account = None


# ----------------------------------------------------------------------
# (11) 路由：/
# ----------------------------------------------------------------------
@app.get("/")
async def index(request: Request):
    return templates.TemplateResponse("welcome.html", {"request": request})


# ----------------------------------------------------------------------
# 启动 FastAPI 应用
# ----------------------------------------------------------------------
if __name__ == "__main__":
    import uvicorn
    
    # 开发环境：使用字符串格式以支持reload
    uvicorn.run(
        "app:app",
        host="0.0.0.0",
        port=5001,
        reload=True,  # 开发时启用
    )
    
    # 生产环境请使用：
    # gunicorn app:app --workers 4 --worker-class uvicorn.workers.UvicornWorker --bind 0.0.0.0:5001
