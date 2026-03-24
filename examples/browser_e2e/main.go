package main

import (
	"context"
	"fmt"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/examples/browser_e2e/other"
)

// ========= 接口实现 =========

type Impl struct{}

// 实现 BrowserModule
func (i *Impl) OpenBrowser(ctx context.Context, url string) (*other.Browser, error) {
	fmt.Println("Mock OpenBrowser:", url)
	return &other.Browser{Name: url}, nil
}

// 实现 BrowserService
func (i *Impl) NewPage(b *other.Browser) (*other.Page, error) {
	fmt.Println("Mock NewPage for browser:", b.Name)
	return &other.Page{URL: "about:blank"}, nil
}

// 实现 PageService
func (i *Impl) Locator(p *other.Page, selectors ...string) (*other.CdpSelector, error) {
	fmt.Printf("Mock Locator on page '%s' with selectors: %v\n", p.URL, selectors)
	id := "unknown"
	if len(selectors) > 0 {
		id = selectors[0]
	}
	return &other.CdpSelector{ID: id}, nil
}

// 实现 CdpSelectorService
func (i *Impl) Click(s *other.CdpSelector) error {
	fmt.Println("Mock Click on selector:", s.ID)
	return nil
}

// ========= 运行测试 =========

func main() {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	impl := &Impl{}
	registry := executor.HandleRegistry()

	// 注册生成的 FFI 路由 (这些函数都在 ffigen.go 中生成)
	RegisterBrowserModule(executor, impl, registry)
	RegisterBrowserService(executor, impl, registry)
	RegisterPageService(executor, impl, registry)
	RegisterCdpSelectorService(executor, impl, registry)

	// Go-Mini 测试脚本
	code := `
	package main
	import "browser" // 导入模块

	func main() {
		// 1. 全局模块方法
		b, err := browser.OpenBrowser("https://powerk8s.cn")
		if err != nil { panic(err.Error()) }

		// 2. Browser 对象方法
		page, err := b.NewPage()
		if err != nil { panic(err.Error()) }

		// 3. Page 对象方法 (测试变长参数 ...)
		// 返回的 btn 是一个 Ptr<CdpSelector>
		btn, err := page.Locator("#su", ".btn-primary", "button")
		if err != nil { panic(err.Error()) }

		// 4. CdpSelector 对象方法
		// 因为 btn 是 Handle，可以继续调用它的专属方法
		err = btn.Click()
		if err != nil { panic(err.Error()) }

		println("E2E Test Passed!")
	}
	`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		panic(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		panic(err)
	}
}
