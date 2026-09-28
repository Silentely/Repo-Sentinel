package auth

import (
	"testing"
)

// TestGetTicketFailsClosedOnEmptyIP 回归：2FA 票据的 IP 绑定必须 fail-closed。
// 原实现在 remoteIP 或 ticket.RemoteIP 任一侧为空时整体跳过绑定，与 setup 环回门、
// metrics 对同一取值的 fail-closed 判定不一致——空取值本就是异常形态，
// 此时放行等于在最需要绑定的路径上放弃绑定。
func TestGetTicketFailsClosedOnEmptyIP(t *testing.T) {
	mgr := NewTOTPTicketManager(3 * 60 * 1000 * 1000 * 1000) // 3 分钟

	// 空 IP 签发的票据：任何地址都不得使用。
	emptyTicket := mgr.CreateTicket("admin-1", "root", "")
	if _, ok := mgr.GetTicket(emptyTicket, "203.0.113.99"); ok {
		t.Fatal("空 IP 签发的票据被其它地址使用（绑定被跳过）")
	}

	// 正常签发的票据：换地址必须失效，同地址可用。
	ticket := mgr.CreateTicket("admin-2", "root", "198.51.100.50")
	if _, ok := mgr.GetTicket(ticket, "198.51.100.51"); ok {
		t.Fatal("票据跨来源 IP 被使用")
	}
	if _, ok := mgr.GetTicket(ticket, "198.51.100.50"); !ok {
		t.Fatal("同来源 IP 应可使用票据")
	}

	// 查询侧 IP 为空同样拒绝。
	if _, ok := mgr.GetTicket(ticket, "  "); ok {
		t.Fatal("查询侧 IP 为空时应拒绝")
	}
}
