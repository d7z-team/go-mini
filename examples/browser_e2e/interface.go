//go:generate go run ../../cmd/ffigen/main.go -pkg main -out ffigen.go interface.go
package main

import (
	"context"

	"gopkg.d7z.net/go-mini/examples/browser_e2e/other"
)

// 1. 全局模块函数
// ffigen:module browser
type BrowserModule interface {
	OpenBrowser(ctx context.Context, url string) (*other.Browser, error)
}

// 2. Browser 对象方法
// ffigen:methods other.Browser
type BrowserService interface {
	NewPage(b *other.Browser) (*other.Page, error)
}

// 3. Page 对象方法
// 注意: 返回值是指针 *CdpSelector, 这样 ffigen 就会把它注册为 Handle, 可以继续链式调用它的方法
// ffigen:methods other.Page
type PageService interface {
	Locator(p *other.Page, selectors ...string) (*other.CdpSelector, error)
}

// 4. CdpSelector 对象方法
// ffigen:methods other.CdpSelector
type CdpSelectorService interface {
	Click(s *other.CdpSelector) error
}
