package pow

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

type Solver interface {
	Warmup() error
	Solve(algorithm, challenge, salt string, difficulty int, expireAt int64, signature, targetPath string) (int64, bool)
}

type DeepSeekHashSolver struct {
	mode string

	mu              sync.Mutex
	inited          bool
	runtime         wazero.Runtime
	module          api.Module
	memory          api.Memory
	addStack        api.Function
	alloc           api.Function
	wasmSolve       api.Function
	wasmPath        string
	stackResultSize uint32
}

func NewSolver() Solver {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv("POW_SOLVER")))
	if mode == "" {
		mode = "wasm"
	}
	wasmPath := strings.TrimSpace(os.Getenv("POW_WASM_PATH"))
	if wasmPath == "" {
		wasmPath = "../sha3_wasm_bg.7b9ca65ddd.wasm"
	}
	return &DeepSeekHashSolver{mode: mode, wasmPath: wasmPath, stackResultSize: 16}
}
func (s *DeepSeekHashSolver) Warmup() error {
	if s.mode == "native" || s.mode == "python" {
		return nil
	}
	return s.initWASM(context.Background())
}

var keccakRC = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808A, 0x8000000080008000,
	0x000000000000808B, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008A, 0x0000000000000088, 0x0000000080008009, 0x000000008000000A,
	0x000000008000808B, 0x800000000000008B, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800A, 0x800000008000000A,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

var rho = [5][5]uint{ // x,y
	{0, 36, 3, 41, 18},
	{1, 44, 10, 45, 2},
	{62, 6, 43, 15, 61},
	{28, 55, 25, 21, 56},
	{27, 20, 39, 8, 14},
}

func keccakF1600Rounds1To23(a *[25]uint64) {
	for round := 1; round < 24; round++ {
		var c [5]uint64
		for x := 0; x < 5; x++ {
			c[x] = a[x] ^ a[x+5] ^ a[x+10] ^ a[x+15] ^ a[x+20]
		}
		var d [5]uint64
		d[0] = c[4] ^ bits.RotateLeft64(c[1], 1)
		d[1] = c[0] ^ bits.RotateLeft64(c[2], 1)
		d[2] = c[1] ^ bits.RotateLeft64(c[3], 1)
		d[3] = c[2] ^ bits.RotateLeft64(c[4], 1)
		d[4] = c[3] ^ bits.RotateLeft64(c[0], 1)
		for x := 0; x < 5; x++ {
			dx := d[x]
			a[x] ^= dx
			a[x+5] ^= dx
			a[x+10] ^= dx
			a[x+15] ^= dx
			a[x+20] ^= dx
		}

		var b [25]uint64
		for x := 0; x < 5; x++ {
			for y := 0; y < 5; y++ {
				b[y+5*((2*x+3*y)%5)] = bits.RotateLeft64(a[x+5*y], int(rho[x][y]))
			}
		}

		for y := 0; y < 5; y++ {
			y5 := 5 * y
			for x := 0; x < 5; x++ {
				a[x+y5] = b[x+y5] ^ ((^b[((x+1)%5)+y5]) & b[((x+2)%5)+y5])
			}
		}
		a[0] ^= keccakRC[round]
	}
}

func deepSeekHashV1(data []byte) [32]byte {
	const rate = 136
	var st [25]uint64
	off := 0
	for off+rate <= len(data) {
		blk := data[off : off+rate]
		for i := 0; i < rate/8; i++ {
			lane := uint64(blk[i*8]) |
				uint64(blk[i*8+1])<<8 |
				uint64(blk[i*8+2])<<16 |
				uint64(blk[i*8+3])<<24 |
				uint64(blk[i*8+4])<<32 |
				uint64(blk[i*8+5])<<40 |
				uint64(blk[i*8+6])<<48 |
				uint64(blk[i*8+7])<<56
			st[i] ^= lane
		}
		keccakF1600Rounds1To23(&st)
		off += rate
	}

	final := make([]byte, rate)
	rem := data[off:]
	copy(final, rem)
	final[len(rem)] = 0x06
	final[rate-1] |= 0x80
	for i := 0; i < rate/8; i++ {
		lane := uint64(final[i*8]) |
			uint64(final[i*8+1])<<8 |
			uint64(final[i*8+2])<<16 |
			uint64(final[i*8+3])<<24 |
			uint64(final[i*8+4])<<32 |
			uint64(final[i*8+5])<<40 |
			uint64(final[i*8+6])<<48 |
			uint64(final[i*8+7])<<56
		st[i] ^= lane
	}
	keccakF1600Rounds1To23(&st)

	var out [32]byte
	for i := 0; i < 4; i++ {
		v := st[i]
		out[i*8+0] = byte(v)
		out[i*8+1] = byte(v >> 8)
		out[i*8+2] = byte(v >> 16)
		out[i*8+3] = byte(v >> 24)
		out[i*8+4] = byte(v >> 32)
		out[i*8+5] = byte(v >> 40)
		out[i*8+6] = byte(v >> 48)
		out[i*8+7] = byte(v >> 56)
	}
	return out
}

func littleEndianInt(b [32]byte) *big.Int {
	rev := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rev[31-i] = b[i]
	}
	return new(big.Int).SetBytes(rev)
}

func targetFromDifficulty(diff int) *big.Int {
	if diff <= 0 {
		return nil
	}
	base := new(big.Int).Lsh(big.NewInt(1), 256)
	return new(big.Int).Div(base, big.NewInt(int64(diff)))
}

func (s *DeepSeekHashSolver) initWASM(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inited {
		return nil
	}
	wasmBytes, err := os.ReadFile(s.wasmPath)
	if err != nil {
		return err
	}
	r := wazero.NewRuntime(ctx)
	mod, err := r.Instantiate(ctx, wasmBytes)
	if err != nil {
		_ = r.Close(ctx)
		return err
	}
	mem := mod.Memory()
	if mem == nil {
		_ = mod.Close(ctx)
		_ = r.Close(ctx)
		return errors.New("wasm memory export not found")
	}
	addStack := mod.ExportedFunction("__wbindgen_add_to_stack_pointer")
	alloc := mod.ExportedFunction("__wbindgen_export_0")
	wasmSolve := mod.ExportedFunction("wasm_solve")
	if addStack == nil || alloc == nil || wasmSolve == nil {
		_ = mod.Close(ctx)
		_ = r.Close(ctx)
		return errors.New("required wasm exports not found")
	}
	s.runtime = r
	s.module = mod
	s.memory = mem
	s.addStack = addStack
	s.alloc = alloc
	s.wasmSolve = wasmSolve
	s.inited = true
	return nil
}

func (s *DeepSeekHashSolver) wasmEncodeString(ctx context.Context, text string) (uint32, uint32, error) {
	b := []byte(text)
	out, err := s.alloc.Call(ctx, uint64(len(b)), 1)
	if err != nil || len(out) == 0 {
		return 0, 0, errors.New("wasm alloc failed")
	}
	ptr := uint32(out[0])
	if ok := s.memory.Write(ptr, b); !ok {
		return 0, 0, errors.New("wasm memory write failed")
	}
	return ptr, uint32(len(b)), nil
}

func (s *DeepSeekHashSolver) solveNative(algorithm, challenge, salt string, difficulty int, expireAt int64, signature, targetPath string) (int64, bool) {
	if strings.TrimSpace(algorithm) != "DeepSeekHashV1" {
		return 0, false
	}
	target := targetFromDifficulty(difficulty)
	if target == nil {
		return 0, false
	}
	prefix := fmt.Sprintf("%s_%d_", salt, expireAt)
	base := challenge + prefix
	for nonce := int64(0); ; nonce++ {
		if expireAt > 0 && time.Now().Unix() >= expireAt {
			return 0, false
		}
		h := deepSeekHashV1([]byte(base + strconv.FormatInt(nonce, 10)))
		if littleEndianInt(h).Cmp(target) < 0 {
			return nonce, true
		}
	}
}

func (s *DeepSeekHashSolver) solveWASM(algorithm, challenge, salt string, difficulty int, expireAt int64, signature, targetPath string) (int64, bool) {
	ctx := context.Background()
	if strings.TrimSpace(algorithm) != "DeepSeekHashV1" {
		return 0, false
	}
	if err := s.initWASM(ctx); err != nil {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stackDelta := int32(-16)
	retPtrRaw, err := s.addStack.Call(ctx, uint64(uint32(stackDelta)))
	if err != nil || len(retPtrRaw) == 0 {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	retPtr := uint32(retPtrRaw[0])
	defer s.addStack.Call(ctx, 16)
	prefix := fmt.Sprintf("%s_%d_", salt, expireAt)
	pChallenge, lChallenge, err := s.wasmEncodeString(ctx, challenge)
	if err != nil {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	pPrefix, lPrefix, err := s.wasmEncodeString(ctx, prefix)
	if err != nil {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	_, err = s.wasmSolve.Call(ctx,
		uint64(retPtr),
		uint64(pChallenge), uint64(lChallenge),
		uint64(pPrefix), uint64(lPrefix),
		math.Float64bits(float64(difficulty)),
	)
	if err != nil {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	statusBytes, ok := s.memory.Read(retPtr, 4)
	if !ok || len(statusBytes) != 4 {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	status := int32(binary.LittleEndian.Uint32(statusBytes))
	if status == 0 {
		return 0, false
	}
	valueBytes, ok := s.memory.Read(retPtr+8, 8)
	if !ok || len(valueBytes) != 8 {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	nonceF := math.Float64frombits(binary.LittleEndian.Uint64(valueBytes))
	return int64(nonceF), true
}

func (s *DeepSeekHashSolver) Solve(algorithm, challenge, salt string, difficulty int, expireAt int64, signature, targetPath string) (int64, bool) {
	if s.mode == "native" || s.mode == "python" {
		return s.solveNative(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
	}
	return s.solveWASM(algorithm, challenge, salt, difficulty, expireAt, signature, targetPath)
}

func HashKey(parts ...string) string {
	h := deepSeekHashV1([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:])
}
