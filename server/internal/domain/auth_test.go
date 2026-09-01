package domain

import (
	"errors"
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
	const ip = "10.0.0.1"
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
					th.RecordFailure(tt.user, ip, at)
				case "success":
					th.RecordSuccess(tt.user, ip)
				case "allow":
					if got := th.Allow(tt.user, ip, at); got != s.want {
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
		th.RecordFailure("locked", "10.0.0.1", now)
	}
	if th.Allow("locked", "10.0.0.1", now) {
		t.Fatal("locked 用户应被拒绝")
	}
	if !th.Allow("other", "10.0.0.1", now) {
		t.Fatal("其他用户不应受影响")
	}
}

// S3：限速按 (用户名, 来源 IP) 计数——攻击者打满自己那格，锁不住同一账号的真实用户。
func TestThrottleIsolatedPerIP(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	th := NewLoginThrottle()
	for i := 0; i < MaxLoginFailures; i++ {
		th.RecordFailure("victim", "10.0.0.66", now)
	}
	if th.Allow("victim", "10.0.0.66", now) {
		t.Fatal("攻击来源 IP 应被拒绝")
	}
	if !th.Allow("victim", "10.0.0.7", now) {
		t.Fatal("同一账号的其他来源 IP 不应被连坐")
	}
}

// 登录成功只清自己这格，不替其他来源 IP 解锁。
func TestThrottleSuccessClearsOwnKeyOnly(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	th := NewLoginThrottle()
	for i := 0; i < MaxLoginFailures; i++ {
		th.RecordFailure("u", "10.0.0.66", now)
	}
	th.RecordSuccess("u", "10.0.0.7")
	if th.Allow("u", "10.0.0.66", now) {
		t.Fatal("另一来源 IP 的成功登录不应替攻击来源解锁")
	}
}

// 失败记录有清理路径：海量随机用户名不再让 map 无限膨胀。
func TestThrottleSweep(t *testing.T) {
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	th := NewLoginThrottle()
	th.RecordFailure("stale", "10.0.0.1", base)
	th.RecordFailure("fresh", "10.0.0.2", base.Add(LoginLockWindow))

	if got := th.Sweep(base.Add(LoginLockWindow + time.Second)); got != 1 {
		t.Fatalf("Sweep 清理条数 = %d, want 1", got)
	}
	if n := th.Size(); n != 1 {
		t.Fatalf("清理后剩余 = %d, want 1", n)
	}
	if got := th.Sweep(base.Add(3 * LoginLockWindow)); got != 1 {
		t.Fatalf("再次 Sweep 清理条数 = %d, want 1", got)
	}
	if n := th.Size(); n != 0 {
		t.Fatalf("清理后剩余 = %d, want 0", n)
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

// S3：改口令的校验规则——当前口令必须正确、新口令至少 8 位且不能与当前口令相同。
func TestValidatePasswordChange(t *testing.T) {
	cases := []struct {
		name    string
		current string
		next    string
		want    error
	}{
		{"合法修改", "old-pass-1", "new-pass-2", nil},
		{"新口令过短", "old-pass-1", "short7x", ErrPasswordTooShort},
		{"新口令与当前相同", "old-pass-1", "old-pass-1", ErrPasswordUnchanged},
		{"新口令为空白", "old-pass-1", "        ", ErrPasswordTooShort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidatePasswordChange(tc.current, tc.next); !errors.Is(got, tc.want) {
				t.Fatalf("ValidatePasswordChange() = %v, want %v", got, tc.want)
			}
		})
	}
}
