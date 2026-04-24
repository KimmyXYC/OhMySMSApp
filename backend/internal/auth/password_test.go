package auth

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePasswordStore 记录最后一次保存的 hash，用于断言持久化路径。
type fakePasswordStore struct {
	mu    sync.Mutex
	hash  string
	calls int
	fail  error
}

func (f *fakePasswordStore) SavePasswordBcrypt(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail != nil {
		return f.fail
	}
	f.hash = hash
	return nil
}

// newPwService 构造带 store 的 Service；initialHash 为空则 dev 模式。
func newPwService(t *testing.T, initialHash string, store PasswordStore) *Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{
		Secret:         []byte(secret),
		Username:       "admin",
		PasswordBcrypt: initialHash,
		TokenTTL:       time.Hour,
		Store:          store,
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// TestChangePasswordSuccess：正常改密，校验内存同步 + store 被调用。
func TestChangePasswordSuccess(t *testing.T) {
	hash, err := HashPassword("oldpw123")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakePasswordStore{}
	svc := newPwService(t, hash, store)

	if err := svc.ChangePassword(context.Background(), "oldpw123", "newpw456"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("expected 1 save call, got %d", store.calls)
	}
	if store.hash == "" {
		t.Fatal("store did not record hash")
	}
	// 新密码能登录
	if err := svc.CheckCredentials("admin", "newpw456"); err != nil {
		t.Fatalf("new password should work: %v", err)
	}
	// 旧密码不能
	if err := svc.CheckCredentials("admin", "oldpw123"); err == nil {
		t.Fatal("old password should not work")
	}
}

// TestChangePasswordWrongCurrent：当前密码错，返回 "invalid current password"。
func TestChangePasswordWrongCurrent(t *testing.T) {
	hash, err := HashPassword("correct-pw")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakePasswordStore{}
	svc := newPwService(t, hash, store)

	err = svc.ChangePassword(context.Background(), "wrong-pw", "newpw789")
	if err == nil {
		t.Fatal("expected error for wrong current password")
	}
	if err.Error() != "invalid current password" {
		t.Fatalf("unexpected err: %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("store should not be called on error, got %d", store.calls)
	}
	// 原密码仍然有效
	if err := svc.CheckCredentials("admin", "correct-pw"); err != nil {
		t.Fatalf("old password must still work: %v", err)
	}
}

// TestChangePasswordTooShort：新密码 < 6 字符返回错误。
func TestChangePasswordTooShort(t *testing.T) {
	hash, _ := HashPassword("somepw-ok")
	svc := newPwService(t, hash, &fakePasswordStore{})

	for _, bad := range []string{"", "     ", "ab", "12345"} {
		err := svc.ChangePassword(context.Background(), "somepw-ok", bad)
		if err == nil {
			t.Fatalf("expected error for new password %q", bad)
		}
		if !strings.Contains(err.Error(), "short") {
			t.Fatalf("unexpected err for %q: %v", bad, err)
		}
	}
}

// TestChangePasswordDevMode：初始 dev 模式（无 hash），任何 current 都通过；之后立即退出 dev。
func TestChangePasswordDevMode(t *testing.T) {
	store := &fakePasswordStore{}
	svc := newPwService(t, "", store)

	if err := svc.ChangePassword(context.Background(), "anything", "firstpw!"); err != nil {
		t.Fatalf("dev-mode change: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("expected 1 save call, got %d", store.calls)
	}
	// 现在必须用正确密码登录
	if err := svc.CheckCredentials("admin", "firstpw!"); err != nil {
		t.Fatalf("new pw login: %v", err)
	}
	if err := svc.CheckCredentials("admin", "random"); err == nil {
		t.Fatal("dev mode should be disabled after setting a password")
	}
}

// TestChangePasswordStoreError：store 报错时 hash 不落内存。
func TestChangePasswordStoreError(t *testing.T) {
	hash, _ := HashPassword("correctpw")
	store := &fakePasswordStore{fail: errors.New("disk full")}
	svc := newPwService(t, hash, store)

	err := svc.ChangePassword(context.Background(), "correctpw", "newpw!!")
	if err == nil {
		t.Fatal("expected store error")
	}
	// 老密码仍可用
	if err := svc.CheckCredentials("admin", "correctpw"); err != nil {
		t.Fatalf("old pw should still work: %v", err)
	}
}
