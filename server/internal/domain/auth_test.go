package domain

import (
	"strings"
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
		t.Fatal("正确密码应通过")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("错误密码不应通过")
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

// S3：改密码的校验规则——当前密码必须正确、新密码至少 8 位且不能与当前密码相同。
func TestValidatePasswordChange(t *testing.T) {
	cases := []struct {
		name    string
		current string
		next    string
		want    error
	}{
		{"合法修改", "old-pass-1", "new-pass-2", nil},
		{"新密码过短", "old-pass-1", "short7x", ErrPasswordTooShort},
		{"新密码与当前相同", "old-pass-1", "old-pass-1", ErrPasswordUnchanged},
		{"新密码为空白", "old-pass-1", "        ", ErrPasswordTooShort},
		// #193：上限 32 位（按 Unicode 字符计）；恰好 8／32 位通过，7／33 位拒绝。
		{"恰好 8 位通过", "old-pass-1", "abcdefgh", nil},
		{"恰好 32 位通过", "old-pass-1", strings.Repeat("a", 32), nil},
		{"33 位过长", "old-pass-1", strings.Repeat("a", 33), ErrPasswordTooLong},
		{"128 位过长（旧契约上限）", "old-pass-1", strings.Repeat("a", 128), ErrPasswordTooLong},
		{"32 个汉字按字符计不超上限但超 72 字节", "old-pass-1", strings.Repeat("密", 32), ErrPasswordTooLong},
		{"24 个汉字（72 字节）通过", "old-pass-1", strings.Repeat("密", 24), nil},
		{"8 个汉字按字符计够 8 位", "old-pass-1", strings.Repeat("密", 8), nil},
		{"7 个汉字按字符计过短（字节数 21 不算数）", "old-pass-1", strings.Repeat("密", 7), ErrPasswordTooShort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidatePasswordChange(tc.current, tc.next); !errors.Is(got, tc.want) {
				t.Fatalf("ValidatePasswordChange() = %v, want %v", got, tc.want)
			}
		})
	}
}

// #208：登录安全的会话列表——保持输入顺序（最新活动在前），当前会话按 token 标出，token 本身不外泄。
func TestSessionViews(t *testing.T) {
	base := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	in := []SessionFact{
		{Token: "tok-b", CreatedAt: base.Add(-time.Hour), LastActiveAt: base, ExpiresAt: base.Add(6 * 24 * time.Hour)},
		{Token: "tok-a", CreatedAt: base.Add(-48 * time.Hour), LastActiveAt: base.Add(-time.Hour), ExpiresAt: base.Add(5 * 24 * time.Hour)},
	}
	got := SessionViews(in, "tok-a")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Current || !got[1].Current {
		t.Fatalf("应只标出 tok-a 为当前会话: %+v", got)
	}
	if !got[0].LastActiveAt.Equal(base) || !got[1].CreatedAt.Equal(base.Add(-48*time.Hour)) || !got[1].ExpiresAt.Equal(base.Add(5*24*time.Hour)) {
		t.Fatalf("时间字段应原样带出: %+v", got)
	}
	if none := SessionViews(in, "unknown"); none[0].Current || none[1].Current {
		t.Fatalf("token 不匹配时不标当前: %+v", none)
	}
	if empty := SessionViews(nil, "tok-a"); len(empty) != 0 {
		t.Fatalf("空输入应得空切片: %+v", empty)
	}
}

// #209：被限速时告知剩余等待秒数——锁定窗口减去距上次失败的时长，向上取整到秒；
// 未被限速为 0。
func TestLoginThrottleRetryAfter(t *testing.T) {
	th := NewLoginThrottle()
	base := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	if got := th.RetryAfter("alice", "1.1.1.1", base); got != 0 {
		t.Fatalf("无失败记录应为 0，得到 %v", got)
	}
	for i := 0; i < MaxLoginFailures; i++ {
		th.RecordFailure("alice", "1.1.1.1", base.Add(time.Duration(i)*time.Second))
	}
	last := base.Add(time.Duration(MaxLoginFailures-1) * time.Second)
	if got := th.RetryAfter("alice", "1.1.1.1", last.Add(10*time.Second)); got != LoginLockWindow-10*time.Second {
		t.Fatalf("剩余等待 = %v, want %v", got, LoginLockWindow-10*time.Second)
	}
	// 未达上限不限速，剩余为 0。
	th2 := NewLoginThrottle()
	th2.RecordFailure("bob", "1.1.1.1", base)
	if got := th2.RetryAfter("bob", "1.1.1.1", base.Add(time.Second)); got != 0 {
		t.Fatalf("未达上限应为 0，得到 %v", got)
	}
	// 锁定窗口过后为 0。
	if got := th.RetryAfter("alice", "1.1.1.1", last.Add(LoginLockWindow+time.Second)); got != 0 {
		t.Fatalf("窗口过后应为 0，得到 %v", got)
	}
	// 亚秒向上取整。
	if got := th.RetryAfter("alice", "1.1.1.1", last.Add(LoginLockWindow-1500*time.Millisecond)); got != 2*time.Second {
		t.Fatalf("应向上取整到 2s，得到 %v", got)
	}
}

