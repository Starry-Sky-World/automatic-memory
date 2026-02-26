package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cloud-sync/internal/config"
	httpapi "cloud-sync/internal/http"
	"cloud-sync/internal/handlers"
	"cloud-sync/internal/repos"
	"cloud-sync/internal/services"
	_ "modernc.org/sqlite"
)

func main() {
	cfg := config.Load()
	db, err := sql.Open("sqlite", cfg.DatabaseURL)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	if err := runMigrations(db, cfg.MigrationsDir); err != nil {
		panic(err)
	}

	repo := repos.NewSyncRepo(db)
	svc := services.NewSyncService(repo)
	h := handlers.NewSyncHandler(svc)
	r := httpapi.NewRouter(cfg, h)

	addr := ":" + cfg.Port
	fmt.Printf("cloud-sync listening on %s\n", addr)
	if err := r.Run(addr); err != nil {
		panic(err)
	}
}

func runMigrations(db *sql.DB, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := applySQLFile(db, path); err != nil {
			return fmt.Errorf("apply migration %s: %w", f, err)
		}
	}
	return nil
}

func applySQLFile(db *sql.DB, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var sb strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	_, err = db.Exec(sb.String())
	return err
}
