package domain

import (
	"testing"
	"time"
)

func TestSessionRenewal(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		wantRenew bool
	}{
		{"刚建立的会话不续期", now.Add(SessionTTL), false},
		{"距满额不足续期间隔不续期", now.Add(SessionTTL - RenewSlack + time.Minute), false},
		{"恰到续期间隔则续期", now.Add(SessionTTL - RenewSlack), true},
		{"已过半的会话续期", now.Add(SessionTTL / 2), true},
		{"临近过期的会话续期", now.Add(time.Minute), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newExpiry, renew := SessionRenewal(now, tt.expiresAt)
			if renew != tt.wantRenew {
				t.Fatalf("renew = %v, want %v", renew, tt.wantRenew)
			}
			if renew && !newExpiry.Equal(now.Add(SessionTTL)) {
				t.Fatalf("newExpiry = %v, want %v", newExpiry, now.Add(SessionTTL))
			}
			if !renew && !newExpiry.Equal(tt.expiresAt) {
				t.Fatalf("newExpiry = %v, want unchanged %v", newExpiry, tt.expiresAt)
			}
		})
	}
}

func TestLoginThrottle(t *testing.T) {
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	type step struct {
		action string // "fail" | "success" | "allow"
		at     time.Duration
		want   bool // 仅 action == "allow" 时断言
	}
	tests := []struct {
		name  string
		user  string
		steps []step
	}{
		{"无失败记录放行", "u1", []step{
			{"allow", 0, true},
		}},
		{"未达上限放行", "u2", []step{
			{"fail", 0, false}, {"fail", time.Second, false}, {"fail", 2 * time.Second, false}, {"fail", 3 * time.Second, false},
			{"allow", 4 * time.Second, true},
		}},
		{"连续失败达上限拒绝", "u3", []step{
			{"fail", 0, false}, {"fail", time.Second, false}, {"fail", 2 * time.Second, false}, {"fail", 3 * time.Second, false}, {"fail", 4 * time.Second, false},
			{"allow", 5 * time.Second, false},
		}},
		{"锁定窗口过后放行", "u4", []step{
			{"fail", 0, false}, {"fail", 0, false}, {"fail", 0, false}, {"fail", 0, false}, {"fail", 0, false},
			{"allow", LoginLockWindow + time.Second, true},
		}},
		{"登录成功清零计数", "u5", []step{
			{"fail", 0, false}, {"fail", 0, false}, {"fail", 0, false}, {"fail", 0, false},
			{"success", time.Second, false},
			{"fail", 2 * time.Second, false},
			{"allow", 3 * time.Second, true},
		}},
		{"久远失败不累计", "u6", []step{
			{"fail", 0, false}, {"fail", 0, false}, {"fail", 0, false}, {"fail", 0, false},
			{"fail", LoginLockWindow + time.Minute, false},
			{"fail", LoginLockWindow + time.Minute, false},
			{"fail", LoginLockWindow + time.Minute, false},
			{"fail", LoginLockWindow + time.Minute, false},
			{"allow", LoginLockWindow + 2*time.Minute, true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := NewLoginThrottle()
			for i, s := range tt.steps {
				at := base.Add(s.at)
				switch s.action {
				case "fail":
					th.RecordFailure(tt.user, at)
				case "success":
					th.RecordSuccess(tt.user)
				case "allow":
					if got := th.Allow(tt.user, at); got != s.want {
						t.Fatalf("step %d: Allow = %v, want %v", i, got, s.want)
					}
				}
			}
		})
	}
}

func TestThrottleIsolatedPerUser(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	th := NewLoginThrottle()
	for i := 0; i < MaxLoginFailures; i++ {
		th.RecordFailure("locked", now)
	}
	if th.Allow("locked", now) {
		t.Fatal("locked 用户应被拒绝")
	}
	if !th.Allow("other", now) {
		t.Fatal("其他用户不应受影响")
	}
}

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "s3cret" {
		t.Fatal("哈希不得等于明文")
	}
	if !VerifyPassword(hash, "s3cret") {
		t.Fatal("正确口令应通过")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("错误口令不应通过")
	}
}

func TestNewSessionToken(t *testing.T) {
	a, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	b, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if len(a) < 32 {
		t.Fatalf("token 过短: %d", len(a))
	}
	if a == b {
		t.Fatal("两次生成的 token 不应相同")
	}
}
