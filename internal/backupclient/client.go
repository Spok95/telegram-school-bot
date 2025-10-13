package backupclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func baseURL() string {
	if v := os.Getenv("BACKUPCTL_URL"); v != "" {
		return v
	}
	return "http://pgbackup:8081"
}

func do(ctx context.Context, path string, timeout time.Duration) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL()+path, nil)
	if err != nil {
		return "", err
	}
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%s: http %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return strings.TrimSpace(string(body)), nil
}

func TriggerBackup(ctx context.Context) (string, error) {
	return do(ctx, "/cgi-bin/backup", 2*time.Minute)
}

func RestoreLatest(ctx context.Context) (string, error) {
	return do(ctx, "/cgi-bin/restore-latest", 5*time.Minute)
}
