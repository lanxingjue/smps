// internal/auth/ip_whitelist.go
package auth

import (
	"net"
	"sync"
)

// IPWhitelist IP白名单
type IPWhitelist struct {
	cidrs []*net.IPNet
	mu    sync.RWMutex
}

// NewIPWhitelist 创建新的IP白名单
func NewIPWhitelist() *IPWhitelist {
	return &IPWhitelist{
		cidrs: make([]*net.IPNet, 0),
	}
}

// AddCIDR 添加CIDR到白名单
func (w *IPWhitelist) AddCIDR(cidr string) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.cidrs = append(w.cidrs, ipNet)
	return nil
}

// AddIP 添加单个IP到白名单
func (w *IPWhitelist) AddIP(ip string) error {
	ipAddr := net.ParseIP(ip)
	if ipAddr == nil {
		return net.InvalidAddrError(ip)
	}

	// 创建包含单个IP的CIDR
	mask := net.CIDRMask(32, 32)
	if ipAddr.To4() == nil {
		mask = net.CIDRMask(128, 128)
	}

	ipNet := &net.IPNet{
		IP:   ipAddr,
		Mask: mask,
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.cidrs = append(w.cidrs, ipNet)
	return nil
}

// Check 检查IP是否在白名单中
func (w *IPWhitelist) Check(ip net.IP) bool {
	// 如果白名单为空，则允许所有IP
	w.mu.RLock()
	defer w.mu.RUnlock()

	if len(w.cidrs) == 0 {
		return true
	}

	for _, cidr := range w.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// Clear 清空白名单
func (w *IPWhitelist) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cidrs = make([]*net.IPNet, 0)
}
