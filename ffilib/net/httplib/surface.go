package httplib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	gohttp "net/http"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

const (
	methodListenAndServe uint32 = iota + 1
	methodListenAndServeAsync
	methodHandleFunc
	methodNewServer
	methodGet
	methodHead
	methodPostBytes
	methodNewRequest
	methodDefaultClient
	methodNewClient
	methodClientDo
	methodClientGet
	methodClientCloseIdleConnections
	methodServerListenAndServe
	methodServerClose
	methodServerShutdown
	methodResponseWriterWrite
	methodResponseWriterWriteHeader
	methodResponseWriterHeaderSet
	methodResponseWriterHeaderAdd
	methodResponseWriterHeaderDel
	methodResponseWriterHeaderGet
	methodRequestMethod
	methodRequestPath
	methodRequestHeaderGet
	methodResponseStatusCode
	methodResponseBody
	methodBodyRead
	methodBodyClose
)

const handlerSig = "function(HostRef<net/http/internal.ResponseWriter>, HostRef<net/http/internal.Request>) Void"

func Surface() *surface.Bundle {
	schema := runtime.NewFFISurfaceSchema()
	for _, route := range httpRoutes {
		if err := schema.AddRouteDecls([]runtime.FFIRouteDecl{route}); err != nil {
			return &surface.Bundle{Err: err}
		}
	}
	for _, item := range httpStructs {
		if err := schema.AddStruct("net/http/internal", item.member, item.spec); err != nil {
			return &surface.Bundle{Err: err}
		}
	}
	return surface.Merge(
		surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
			host := &httpHost{registry: ctx.Registry}
			var bridge *ffigo.RouterBridge
			bridge = ffigo.NewRouterBridge(ctx.Registry, func(callCtx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
				host.bridge = bridge
				return host.route(callCtx, req)
			})
			bound := runtime.NewBoundFFISurfaceFromSchema(schema)
			if err := bound.BindSchemaRoutes(schema, bridge); err != nil {
				return nil, err
			}
			return bound, nil
		}),
		surface.Library("net/http", surface.GoFile("http.mgo", httpSource)),
	)
}

var httpRoutes = []runtime.FFIRouteDecl{
	{PackagePath: "net/http/internal", MemberName: "ListenAndServe", RouteName: "net/http/internal.ListenAndServe", MethodID: methodListenAndServe, Sig: runtime.MustParseRuntimeFuncSig("function(String, " + handlerSig + ") Error")},
	{PackagePath: "net/http/internal", MemberName: "ListenAndServeAsync", RouteName: "net/http/internal.ListenAndServeAsync", MethodID: methodListenAndServeAsync, Sig: runtime.MustParseRuntimeFuncSig("function(String, " + handlerSig + ") tuple(HostRef<net/http/internal.Server>, Error)")},
	{PackagePath: "net/http/internal", MemberName: "HandleFunc", RouteName: "net/http/internal.HandleFunc", MethodID: methodHandleFunc, Sig: runtime.MustParseRuntimeFuncSig("function(String, " + handlerSig + ") Void")},
	{PackagePath: "net/http/internal", MemberName: "NewServer", RouteName: "net/http/internal.NewServer", MethodID: methodNewServer, Sig: runtime.MustParseRuntimeFuncSig("function(String, " + handlerSig + ") HostRef<net/http/internal.Server>")},
	{PackagePath: "net/http/internal", MemberName: "Get", RouteName: "net/http/internal.Get", MethodID: methodGet, Sig: runtime.MustParseRuntimeFuncSig("function(String) tuple(HostRef<net/http/internal.Response>, Error)")},
	{PackagePath: "net/http/internal", MemberName: "Head", RouteName: "net/http/internal.Head", MethodID: methodHead, Sig: runtime.MustParseRuntimeFuncSig("function(String) tuple(HostRef<net/http/internal.Response>, Error)")},
	{PackagePath: "net/http/internal", MemberName: "PostBytes", RouteName: "net/http/internal.PostBytes", MethodID: methodPostBytes, Sig: runtime.MustParseRuntimeFuncSig("function(String, String, Array<Byte>) tuple(HostRef<net/http/internal.Response>, Error)")},
	{PackagePath: "net/http/internal", MemberName: "NewRequest", RouteName: "net/http/internal.NewRequest", MethodID: methodNewRequest, Sig: runtime.MustParseRuntimeFuncSig("function(String, String, Array<Byte>) tuple(HostRef<net/http/internal.Request>, Error)")},
	{PackagePath: "net/http/internal", MemberName: "DefaultClient", RouteName: "net/http/internal.DefaultClient", MethodID: methodDefaultClient, Sig: runtime.MustParseRuntimeFuncSig("function() HostRef<net/http/internal.Client>")},
	{PackagePath: "net/http/internal", MemberName: "NewClient", RouteName: "net/http/internal.NewClient", MethodID: methodNewClient, Sig: runtime.MustParseRuntimeFuncSig("function() HostRef<net/http/internal.Client>")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Client", MethodName: "Do", RouteName: "net/http/internal.Client.Do", MethodID: methodClientDo, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Client>, HostRef<net/http/internal.Request>) tuple(HostRef<net/http/internal.Response>, Error)")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Client", MethodName: "Get", RouteName: "net/http/internal.Client.Get", MethodID: methodClientGet, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Client>, String) tuple(HostRef<net/http/internal.Response>, Error)")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Client", MethodName: "CloseIdleConnections", RouteName: "net/http/internal.Client.CloseIdleConnections", MethodID: methodClientCloseIdleConnections, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Client>) Void")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Server", MethodName: "ListenAndServe", RouteName: "net/http/internal.Server.ListenAndServe", MethodID: methodServerListenAndServe, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Server>) Error")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Server", MethodName: "Close", RouteName: "net/http/internal.Server.Close", MethodID: methodServerClose, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Server>) Error")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Server", MethodName: "Shutdown", RouteName: "net/http/internal.Server.Shutdown", MethodID: methodServerShutdown, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Server>) Error")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "ResponseWriter", MethodName: "Write", RouteName: "net/http/internal.ResponseWriter.Write", MethodID: methodResponseWriterWrite, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.ResponseWriter>, Array<Byte>) tuple(Int64, Error)")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "ResponseWriter", MethodName: "WriteHeader", RouteName: "net/http/internal.ResponseWriter.WriteHeader", MethodID: methodResponseWriterWriteHeader, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.ResponseWriter>, Int64) Void")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "ResponseWriter", MethodName: "HeaderSet", RouteName: "net/http/internal.ResponseWriter.HeaderSet", MethodID: methodResponseWriterHeaderSet, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.ResponseWriter>, String, String) Void")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "ResponseWriter", MethodName: "HeaderAdd", RouteName: "net/http/internal.ResponseWriter.HeaderAdd", MethodID: methodResponseWriterHeaderAdd, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.ResponseWriter>, String, String) Void")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "ResponseWriter", MethodName: "HeaderDel", RouteName: "net/http/internal.ResponseWriter.HeaderDel", MethodID: methodResponseWriterHeaderDel, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.ResponseWriter>, String) Void")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "ResponseWriter", MethodName: "HeaderGet", RouteName: "net/http/internal.ResponseWriter.HeaderGet", MethodID: methodResponseWriterHeaderGet, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.ResponseWriter>, String) String")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Request", MethodName: "Method", RouteName: "net/http/internal.Request.Method", MethodID: methodRequestMethod, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Request>) String")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Request", MethodName: "Path", RouteName: "net/http/internal.Request.Path", MethodID: methodRequestPath, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Request>) String")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Request", MethodName: "HeaderGet", RouteName: "net/http/internal.Request.HeaderGet", MethodID: methodRequestHeaderGet, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Request>, String) String")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Response", MethodName: "StatusCode", RouteName: "net/http/internal.Response.StatusCode", MethodID: methodResponseStatusCode, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Response>) Int64")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Response", MethodName: "Body", RouteName: "net/http/internal.Response.Body", MethodID: methodResponseBody, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Response>) HostRef<net/http/internal.Body>")},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Body", MethodName: "Read", RouteName: "net/http/internal.Body.Read", MethodID: methodBodyRead, Sig: runtime.MustParseRuntimeFuncSigWithModes("function(HostRef<net/http/internal.Body>, Array<Byte>) tuple(Int64, Error)", runtime.FFIParamIn, runtime.FFIParamInOutBytes)},
	{TypePackagePath: "net/http/internal", TypeMemberName: "Body", MethodName: "Close", RouteName: "net/http/internal.Body.Close", MethodID: methodBodyClose, Sig: runtime.MustParseRuntimeFuncSig("function(HostRef<net/http/internal.Body>) Error")},
}

var httpStructs = []struct {
	member string
	spec   *runtime.RuntimeStructSpec
}{
	{"Server", runtime.MustParseRuntimeStructSpec("net/http/internal.Server", runtime.StructOwnershipHostOpaque, "struct { ListenAndServe function(HostRef<net/http/internal.Server>) Error; Close function(HostRef<net/http/internal.Server>) Error; Shutdown function(HostRef<net/http/internal.Server>) Error; }")},
	{"Client", runtime.MustParseRuntimeStructSpec("net/http/internal.Client", runtime.StructOwnershipHostOpaque, "struct { Do function(HostRef<net/http/internal.Client>, HostRef<net/http/internal.Request>) tuple(HostRef<net/http/internal.Response>, Error); Get function(HostRef<net/http/internal.Client>, String) tuple(HostRef<net/http/internal.Response>, Error); CloseIdleConnections function(HostRef<net/http/internal.Client>) Void; }")},
	{"ResponseWriter", runtime.MustParseRuntimeStructSpec("net/http/internal.ResponseWriter", runtime.StructOwnershipHostOpaque, "struct { Write function(HostRef<net/http/internal.ResponseWriter>, Array<Byte>) tuple(Int64, Error); WriteHeader function(HostRef<net/http/internal.ResponseWriter>, Int64) Void; HeaderSet function(HostRef<net/http/internal.ResponseWriter>, String, String) Void; HeaderAdd function(HostRef<net/http/internal.ResponseWriter>, String, String) Void; HeaderDel function(HostRef<net/http/internal.ResponseWriter>, String) Void; HeaderGet function(HostRef<net/http/internal.ResponseWriter>, String) String; }")},
	{"Request", runtime.MustParseRuntimeStructSpec("net/http/internal.Request", runtime.StructOwnershipHostOpaque, "struct { Method function(HostRef<net/http/internal.Request>) String; Path function(HostRef<net/http/internal.Request>) String; HeaderGet function(HostRef<net/http/internal.Request>, String) String; }")},
	{"Response", runtime.MustParseRuntimeStructSpec("net/http/internal.Response", runtime.StructOwnershipHostOpaque, "struct { StatusCode function(HostRef<net/http/internal.Response>) Int64; Body function(HostRef<net/http/internal.Response>) HostRef<net/http/internal.Body>; }")},
	{"Body", runtime.MustParseRuntimeStructSpec("net/http/internal.Body", runtime.StructOwnershipHostOpaque, "struct { Read function(HostRef<net/http/internal.Body>, Array<Byte>) tuple(Int64, Error); Close function(HostRef<net/http/internal.Body>) Error; }")},
}

type httpHost struct {
	registry *ffigo.HandleRegistry
	bridge   *ffigo.RouterBridge
}

type httpServer struct {
	srv       *gohttp.Server
	handler   *runtime.VMCallbackProxy
	serviceID uint64
	mu        sync.Mutex
}

type responseWriter struct {
	w    gohttp.ResponseWriter
	mu   sync.Mutex
	live bool
}

type requestRef struct {
	req *gohttp.Request
}

type responseRef struct {
	resp *gohttp.Response
}

type bodyRef struct {
	body io.ReadCloser
}

type clientRef struct {
	client *gohttp.Client
}

func (h *httpHost) route(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	r := ffigo.NewReader(req.Args)
	switch req.MethodID {
	case methodListenAndServe:
		addr, _ := r.ReadString()
		cb, err := h.readCallback(r)
		if err != nil {
			return nil, err
		}
		server := h.newServer(addr, cb)
		return errorAsync("net/http.ListenAndServe", server.listenAndServe()), nil
	case methodListenAndServeAsync:
		addr, _ := r.ReadString()
		cb, err := h.readCallback(r)
		if err != nil {
			return nil, err
		}
		server := h.newServer(addr, cb)
		if err := server.start(); err != nil {
			return h.responseServer(nil, err), nil
		}
		return h.responseServer(server, nil), nil
	case methodHandleFunc:
		pattern, _ := r.ReadString()
		cb, err := h.readCallback(r)
		if err != nil {
			return nil, err
		}
		gohttp.Handle(pattern, h.handler(cb))
		return nil, nil
	case methodNewServer:
		addr, _ := r.ReadString()
		cb, err := h.readCallback(r)
		if err != nil {
			return nil, err
		}
		return h.hostRef("net/http/internal.Server", h.newServer(addr, cb)), nil
	case methodGet:
		url, _ := r.ReadString()
		return responseAsync("net/http.Get", h.registry, func(ctx context.Context) (*gohttp.Response, error) {
			request, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			return gohttp.DefaultClient.Do(request)
		}), nil
	case methodHead:
		url, _ := r.ReadString()
		return responseAsync("net/http.Head", h.registry, func(ctx context.Context) (*gohttp.Response, error) {
			request, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodHead, url, nil)
			if err != nil {
				return nil, err
			}
			return gohttp.DefaultClient.Do(request)
		}), nil
	case methodPostBytes:
		url, _ := r.ReadString()
		contentType, _ := r.ReadString()
		body, _ := r.ReadBytes()
		return responseAsync("net/http.PostBytes", h.registry, func(ctx context.Context) (*gohttp.Response, error) {
			request, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodPost, url, strings.NewReader(string(body)))
			if err != nil {
				return nil, err
			}
			request.Header.Set("Content-Type", contentType)
			return gohttp.DefaultClient.Do(request)
		}), nil
	case methodNewRequest:
		method, _ := r.ReadString()
		url, _ := r.ReadString()
		body, _ := r.ReadBytes()
		request, err := gohttp.NewRequestWithContext(ctx, method, url, strings.NewReader(string(body)))
		return h.responseRequest(request, err), nil
	case methodDefaultClient:
		return h.hostRef("net/http/internal.Client", &clientRef{client: gohttp.DefaultClient}), nil
	case methodNewClient:
		return h.hostRef("net/http/internal.Client", &clientRef{client: &gohttp.Client{}}), nil
	case methodClientDo:
		rawClient, err := h.readHost(r, "net/http/internal.Client")
		if err != nil {
			return nil, err
		}
		rawRequest, err := h.readHost(r, "net/http/internal.Request")
		if err != nil {
			return nil, err
		}
		client := rawClient.(*clientRef)
		request := rawRequest.(*requestRef)
		return responseAsync("net/http.Client.Do", h.registry, func(ctx context.Context) (*gohttp.Response, error) {
			return client.client.Do(request.req.WithContext(ctx))
		}), nil
	case methodClientGet:
		rawClient, err := h.readHost(r, "net/http/internal.Client")
		if err != nil {
			return nil, err
		}
		url, _ := r.ReadString()
		client := rawClient.(*clientRef)
		return responseAsync("net/http.Client.Get", h.registry, func(ctx context.Context) (*gohttp.Response, error) {
			request, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			return client.client.Do(request)
		}), nil
	case methodClientCloseIdleConnections:
		rawClient, err := h.readHost(r, "net/http/internal.Client")
		if err != nil {
			return nil, err
		}
		rawClient.(*clientRef).client.CloseIdleConnections()
		return nil, nil
	case methodServerListenAndServe:
		rawServer, err := h.readHost(r, "net/http/internal.Server")
		if err != nil {
			return nil, err
		}
		server := rawServer.(*httpServer)
		return errorAsync("net/http.Server.ListenAndServe", server.listenAndServe()), nil
	case methodServerClose:
		rawServer, err := h.readHost(r, "net/http/internal.Server")
		if err != nil {
			return nil, err
		}
		server := rawServer.(*httpServer)
		return encodeError(server.Close()), nil
	case methodServerShutdown:
		rawServer, err := h.readHost(r, "net/http/internal.Server")
		if err != nil {
			return nil, err
		}
		server := rawServer.(*httpServer)
		return errorAsync("net/http.Server.Shutdown", func(ctx context.Context) error { return server.Shutdown(ctx) }), nil
	case methodResponseWriterWrite:
		rawWriter, err := h.readHost(r, "net/http/internal.ResponseWriter")
		if err != nil {
			return nil, err
		}
		w := rawWriter.(*responseWriter)
		data, _ := r.ReadBytes()
		n, err := w.Write(data)
		return encodeIntError(int64(n), err), nil
	case methodResponseWriterWriteHeader:
		rawWriter, err := h.readHost(r, "net/http/internal.ResponseWriter")
		if err != nil {
			return nil, err
		}
		w := rawWriter.(*responseWriter)
		code, _ := r.ReadVarint()
		w.WriteHeader(int(code))
		return nil, nil
	case methodResponseWriterHeaderSet:
		rawWriter, err := h.readHost(r, "net/http/internal.ResponseWriter")
		if err != nil {
			return nil, err
		}
		w := rawWriter.(*responseWriter)
		key, _ := r.ReadString()
		value, _ := r.ReadString()
		w.HeaderSet(key, value)
		return nil, nil
	case methodResponseWriterHeaderAdd:
		rawWriter, err := h.readHost(r, "net/http/internal.ResponseWriter")
		if err != nil {
			return nil, err
		}
		w := rawWriter.(*responseWriter)
		key, _ := r.ReadString()
		value, _ := r.ReadString()
		w.HeaderAdd(key, value)
		return nil, nil
	case methodResponseWriterHeaderDel:
		rawWriter, err := h.readHost(r, "net/http/internal.ResponseWriter")
		if err != nil {
			return nil, err
		}
		w := rawWriter.(*responseWriter)
		key, _ := r.ReadString()
		w.HeaderDel(key)
		return nil, nil
	case methodResponseWriterHeaderGet:
		rawWriter, err := h.readHost(r, "net/http/internal.ResponseWriter")
		if err != nil {
			return nil, err
		}
		w := rawWriter.(*responseWriter)
		key, _ := r.ReadString()
		return encodeString(w.HeaderGet(key)), nil
	case methodRequestMethod:
		rawRequest, err := h.readHost(r, "net/http/internal.Request")
		if err != nil {
			return nil, err
		}
		request := rawRequest.(*requestRef)
		return encodeString(request.req.Method), nil
	case methodRequestPath:
		rawRequest, err := h.readHost(r, "net/http/internal.Request")
		if err != nil {
			return nil, err
		}
		request := rawRequest.(*requestRef)
		if request.req.URL == nil {
			return encodeString(""), nil
		}
		return encodeString(request.req.URL.Path), nil
	case methodRequestHeaderGet:
		rawRequest, err := h.readHost(r, "net/http/internal.Request")
		if err != nil {
			return nil, err
		}
		request := rawRequest.(*requestRef)
		key, _ := r.ReadString()
		return encodeString(request.req.Header.Get(key)), nil
	case methodResponseStatusCode:
		rawResponse, err := h.readHost(r, "net/http/internal.Response")
		if err != nil {
			return nil, err
		}
		response := rawResponse.(*responseRef)
		return encodeInt(int64(response.resp.StatusCode)), nil
	case methodResponseBody:
		rawResponse, err := h.readHost(r, "net/http/internal.Response")
		if err != nil {
			return nil, err
		}
		response := rawResponse.(*responseRef)
		return h.hostRef("net/http/internal.Body", &bodyRef{body: response.resp.Body}), nil
	case methodBodyRead:
		rawBody, err := h.readHost(r, "net/http/internal.Body")
		if err != nil {
			return nil, err
		}
		body := rawBody.(*bodyRef)
		buf, _ := r.ReadBytes()
		n, err := body.body.Read(buf)
		return encodeBytesIntError(buf, int64(n), err), nil
	case methodBodyClose:
		rawBody, err := h.readHost(r, "net/http/internal.Body")
		if err != nil {
			return nil, err
		}
		body := rawBody.(*bodyRef)
		return encodeError(body.body.Close()), nil
	default:
		return nil, fmt.Errorf("unknown net/http method ID %d", req.MethodID)
	}
}

func (h *httpHost) readCallback(r *ffigo.Reader) (*runtime.VMCallbackProxy, error) {
	cb, err := r.ReadRawCallback()
	if err != nil {
		return nil, err
	}
	if cb.Handle == 0 {
		return nil, errors.New("net/http: missing handler callback")
	}
	obj, err := h.registry.GetWithAudit(cb.Handle)
	if err != nil {
		return nil, err
	}
	proxy, ok := obj.(*runtime.VMCallbackProxy)
	if !ok {
		return nil, fmt.Errorf("net/http: callback handle has unexpected type %T", obj)
	}
	return proxy, nil
}

func (h *httpHost) readHost(r *ffigo.Reader, typeID string) (any, error) {
	raw, _ := r.ReadUvarint()
	if raw == 0 {
		return nil, fmt.Errorf("net/http: nil host reference %s", typeID)
	}
	obj, err := h.registry.GetTypedWithAudit(uint32(raw), typeID)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (h *httpHost) hostRef(typeID string, obj any) []byte {
	id := h.registry.RegisterTyped(obj, typeID)
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteUvarint(uint64(id))
	return append([]byte(nil), buf.Bytes()...)
}

func (h *httpHost) newServer(addr string, cb *runtime.VMCallbackProxy) *httpServer {
	server := &httpServer{handler: cb}
	server.srv = &gohttp.Server{Addr: addr, Handler: h.handler(cb)}
	return server
}

func (h *httpHost) handler(cb *runtime.VMCallbackProxy) gohttp.Handler {
	return gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		writer := &responseWriter{w: w, live: true}
		wid := h.registry.RegisterTyped(writer, "net/http/internal.ResponseWriter")
		rid := h.registry.RegisterTyped(&requestRef{req: r}, "net/http/internal.Request")
		defer func() {
			writer.mu.Lock()
			writer.live = false
			writer.mu.Unlock()
			h.registry.Remove(wid)
			h.registry.Remove(rid)
		}()
		res := cb.Invoke(r.Context(), []*runtime.Var{
			runtime.NewHostRefVar(wid, h.bridge, "net/http/internal.ResponseWriter"),
			runtime.NewHostRefVar(rid, h.bridge, "net/http/internal.Request"),
		})
		if res.Err != nil {
			gohttp.Error(w, res.Err.Error(), gohttp.StatusInternalServerError)
		}
	})
}

func (h *httpHost) responseRequest(req *gohttp.Request, err error) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	if err != nil {
		buf.WriteUvarint(0)
		buf.WriteRawError(err.Error(), 0)
		return append([]byte(nil), buf.Bytes()...)
	}
	id := h.registry.RegisterTyped(&requestRef{req: req}, "net/http/internal.Request")
	buf.WriteUvarint(uint64(id))
	buf.WriteRawError("", 0)
	return append([]byte(nil), buf.Bytes()...)
}

func (h *httpHost) responseServer(server *httpServer, err error) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	if err != nil || server == nil {
		buf.WriteUvarint(0)
		if err != nil {
			buf.WriteRawError(err.Error(), 0)
		} else {
			buf.WriteRawError("net/http: nil server", 0)
		}
		return append([]byte(nil), buf.Bytes()...)
	}
	id := h.registry.RegisterTyped(server, "net/http/internal.Server")
	buf.WriteUvarint(uint64(id))
	buf.WriteRawError("", 0)
	return append([]byte(nil), buf.Bytes()...)
}

func (s *httpServer) start() error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}
	s.registerService()
	go func() {
		defer s.unregisterService()
		defer func() {
			if s.handler != nil {
				s.handler.ReleaseHostHandle()
			}
		}()
		_ = s.serve(ln)
	}()
	return nil
}

func (s *httpServer) listenAndServe() func(context.Context) error {
	return s.listenWithCancel
}

func (s *httpServer) listenWithCancel(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}
	s.registerService()
	defer s.unregisterService()
	defer func() {
		if s.handler != nil {
			s.handler.ReleaseHostHandle()
		}
	}()
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.serve(ln)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		_ = s.Close()
		err := <-errCh
		if err == nil || errors.Is(err, gohttp.ErrServerClosed) {
			return ctx.Err()
		}
		return err
	}
}

func (s *httpServer) serve(ln net.Listener) error {
	err := s.srv.Serve(ln)
	if errors.Is(err, gohttp.ErrServerClosed) {
		return err
	}
	return err
}

func (s *httpServer) Close() error {
	if s == nil || s.srv == nil {
		return nil
	}
	err := s.srv.Close()
	if s.handler != nil {
		s.handler.ReleaseHostHandle()
	}
	s.unregisterService()
	return err
}

func (s *httpServer) Shutdown(ctx context.Context) error {
	if s == nil || s.srv == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := s.srv.Shutdown(ctx)
	if s.handler != nil {
		s.handler.ReleaseHostHandle()
	}
	s.unregisterService()
	return err
}

func (s *httpServer) registerService() {
	if s == nil || s.handler == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serviceID == 0 {
		s.serviceID = s.handler.RegisterHostService(s)
	}
}

func (s *httpServer) unregisterService() {
	if s == nil || s.handler == nil {
		return
	}
	s.mu.Lock()
	id := s.serviceID
	s.serviceID = 0
	s.mu.Unlock()
	if id != 0 {
		s.handler.UnregisterHostService(id)
	}
}

func (w *responseWriter) HeaderSet(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live && w.w != nil {
		w.w.Header().Set(key, value)
	}
}

func (w *responseWriter) HeaderAdd(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live && w.w != nil {
		w.w.Header().Add(key, value)
	}
}

func (w *responseWriter) HeaderDel(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live && w.w != nil {
		w.w.Header().Del(key)
	}
}

func (w *responseWriter) HeaderGet(key string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live && w.w != nil {
		return w.w.Header().Get(key)
	}
	return ""
}

func (w *responseWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live && w.w != nil {
		w.w.WriteHeader(code)
	}
}

func (w *responseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.live || w.w == nil {
		return 0, errors.New("net/http: response writer is no longer valid")
	}
	return w.w.Write(data)
}

func errorAsync(reason string, run func(context.Context) error) ffigo.AsyncCall {
	return ffigo.AsyncValue[error](ffigo.AsyncFunc[error](func(ctx context.Context, done ffigo.Completion[error]) (ffigo.WaitHandle, error) {
		runCtx, cancel := context.WithCancel(ctx)
		go func() {
			done.Complete(run(runCtx), nil)
		}()
		return ffigo.NewWaitHandle(ffigo.WaitExternal, reason, cancel), nil
	}), func(buf *ffigo.Buffer, err error) error {
		if err != nil {
			buf.WriteRawError(err.Error(), 0)
		} else {
			buf.WriteRawError("", 0)
		}
		return nil
	})
}

func responseAsync(reason string, registry *ffigo.HandleRegistry, run func(context.Context) (*gohttp.Response, error)) ffigo.AsyncCall {
	type responseResult = ffigo.Tuple2[*gohttp.Response, error]
	return ffigo.AsyncValue[responseResult](ffigo.AsyncFunc[responseResult](func(ctx context.Context, done ffigo.Completion[responseResult]) (ffigo.WaitHandle, error) {
		runCtx, cancel := context.WithCancel(ctx)
		go func() {
			resp, err := run(runCtx)
			done.Complete(responseResult{V0: resp, V1: err}, nil)
		}()
		return ffigo.NewWaitHandle(ffigo.WaitExternal, reason, cancel), nil
	}), func(buf *ffigo.Buffer, result responseResult) error {
		if result.V0 == nil {
			buf.WriteUvarint(0)
		} else {
			buf.WriteUvarint(uint64(registry.RegisterTyped(&responseRef{resp: result.V0}, "net/http/internal.Response")))
		}
		if result.V1 != nil {
			buf.WriteRawError(result.V1.Error(), 0)
		} else {
			buf.WriteRawError("", 0)
		}
		return nil
	})
}

func encodeString(s string) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteString(s)
	return append([]byte(nil), buf.Bytes()...)
}

func encodeInt(v int64) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteVarint(v)
	return append([]byte(nil), buf.Bytes()...)
}

func encodeError(err error) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	if err != nil {
		buf.WriteRawError(err.Error(), 0)
	} else {
		buf.WriteRawError("", 0)
	}
	return append([]byte(nil), buf.Bytes()...)
}

func encodeIntError(n int64, err error) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteVarint(n)
	if err != nil {
		buf.WriteRawError(err.Error(), 0)
	} else {
		buf.WriteRawError("", 0)
	}
	return append([]byte(nil), buf.Bytes()...)
}

func encodeBytesIntError(data []byte, n int64, err error) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteUvarint(1)
	buf.WriteBytes(data)
	buf.WriteVarint(n)
	if err != nil {
		buf.WriteRawError(err.Error(), 0)
	} else {
		buf.WriteRawError("", 0)
	}
	return append([]byte(nil), buf.Bytes()...)
}

const httpSource = `
package http

import internal "net/http/internal"

const MethodGet = "GET"
const MethodHead = "HEAD"
const MethodPost = "POST"
const MethodPut = "PUT"
const MethodPatch = "PATCH"
const MethodDelete = "DELETE"
const MethodConnect = "CONNECT"
const MethodOptions = "OPTIONS"
const MethodTrace = "TRACE"

const StatusOK = 200
const StatusCreated = 201
const StatusBadRequest = 400
const StatusNotFound = 404
const StatusInternalServerError = 500

type HandlerFunc func(*ResponseWriter, *Request)

type Header struct {
	writer *internal.ResponseWriter
	request *internal.Request
}

func (h *Header) Set(key string, value string) {
	if h == nil {
		return
	}
	if h.writer != nil {
		h.writer.HeaderSet(key, value)
	}
}

func (h *Header) Add(key string, value string) {
	if h == nil {
		return
	}
	if h.writer != nil {
		h.writer.HeaderAdd(key, value)
	}
}

func (h *Header) Get(key string) string {
	if h == nil {
		return ""
	}
	if h.writer != nil {
		return h.writer.HeaderGet(key)
	}
	if h.request != nil {
		return h.request.HeaderGet(key)
	}
	return ""
}

func (h *Header) Del(key string) {
	if h == nil {
		return
	}
	if h.writer != nil {
		h.writer.HeaderDel(key)
	}
}

func HeaderSet(h *Header, key string, value string) {
	h.Set(key, value)
}

func HeaderAdd(h *Header, key string, value string) {
	h.Add(key, value)
}

func HeaderDel(h *Header, key string) {
	h.Del(key)
}

func HeaderGet(h *Header, key string) string {
	return h.Get(key)
}

type ResponseWriter struct {
	inner *internal.ResponseWriter
}

func (w *ResponseWriter) Header() *Header {
	return &Header{writer: w.inner}
}

func (w *ResponseWriter) Write(b []byte) (int64, error) {
	return w.inner.Write(b)
}

func (w *ResponseWriter) WriteHeader(statusCode int64) {
	w.inner.WriteHeader(statusCode)
}

func WriterHeader(w *ResponseWriter) *Header {
	return w.Header()
}

func Write(w *ResponseWriter, b []byte) (int64, error) {
	return w.Write(b)
}

func WriteHeader(w *ResponseWriter, statusCode int64) {
	w.WriteHeader(statusCode)
}

type Request struct {
	inner *internal.Request
}

func (r *Request) Method() string {
	return r.inner.Method()
}

func (r *Request) Path() string {
	return r.inner.Path()
}

func (r *Request) Header() *Header {
	return &Header{request: r.inner}
}

func (r *Request) HeaderGet(key string) string {
	return r.inner.HeaderGet(key)
}

func Method(r *Request) string {
	return r.Method()
}

func Path(r *Request) string {
	return r.Path()
}

func RequestHeader(r *Request) *Header {
	return r.Header()
}

type Body struct {
	inner *internal.Body
}

func (b Body) Read(buf []byte) (int64, error) {
	return b.inner.Read(buf)
}

func (b Body) Close() error {
	return b.inner.Close()
}

func ReadBody(b Body, buf []byte) (int64, error) {
	return b.Read(buf)
}

func CloseBody(b Body) error {
	return b.Close()
}

type Response struct {
	inner *internal.Response
}

func (r Response) StatusCode() int64 {
	return r.inner.StatusCode()
}

func (r Response) Body() Body {
	return Body{inner: r.inner.Body()}
}

func StatusCode(r Response) int64 {
	return r.StatusCode()
}

func BodyOf(r Response) Body {
	return r.Body()
}

type Server struct {
	inner *internal.Server
}

type Client struct {
	inner *internal.Client
}

func (s Server) ListenAndServe() error {
	return s.inner.ListenAndServe()
}

func (s Server) Close() error {
	return s.inner.Close()
}

func (s Server) Shutdown() error {
	return s.inner.Shutdown()
}

func CloseServer(s Server) error {
	return s.Close()
}

func ShutdownServer(s Server) error {
	return s.Shutdown()
}

func adaptFunc(handler func(*ResponseWriter, *Request)) func(*internal.ResponseWriter, *internal.Request) {
	return func(w *internal.ResponseWriter, r *internal.Request) {
		handler(&ResponseWriter{inner: w}, &Request{inner: r})
	}
}

func ListenAndServe(addr string, handler func(*ResponseWriter, *Request)) error {
	return internal.ListenAndServe(addr, adaptFunc(handler))
}

func ListenAndServeAsync(addr string, handler func(*ResponseWriter, *Request)) (Server, error) {
	srv, err := internal.ListenAndServeAsync(addr, adaptFunc(handler))
	if err != nil {
		return Server{}, err
	}
	return Server{inner: srv}, nil
}

func NewServer(addr string, handler func(*ResponseWriter, *Request)) Server {
	return Server{inner: internal.NewServer(addr, adaptFunc(handler))}
}

func HandleFunc(pattern string, handler func(*ResponseWriter, *Request)) {
	internal.HandleFunc(pattern, adaptFunc(handler))
}

func Get(url string) (Response, error) {
	resp, err := internal.Get(url)
	if err != nil {
		return Response{}, err
	}
	return Response{inner: resp}, nil
}

func NewRequest(method string, url string, body []byte) (*Request, error) {
	req, err := internal.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	return &Request{inner: req}, nil
}

func NewRequestBytes(method string, url string, body []byte) (*Request, error) {
	return NewRequest(method, url, body)
}

func Head(url string) (Response, error) {
	resp, err := internal.Head(url)
	if err != nil {
		return Response{}, err
	}
	return Response{inner: resp}, nil
}

func PostBytes(url string, contentType string, body []byte) (Response, error) {
	resp, err := internal.PostBytes(url, contentType, body)
	if err != nil {
		return Response{}, err
	}
	return Response{inner: resp}, nil
}

func DefaultClient() Client {
	return Client{inner: internal.DefaultClient()}
}

func NewClient() Client {
	return Client{inner: internal.NewClient()}
}

func ClientDo(c Client, req *Request) (Response, error) {
	resp, err := c.inner.Do(req.inner)
	if err != nil {
		return Response{}, err
	}
	return Response{inner: resp}, nil
}

func ClientGet(c Client, url string) (Response, error) {
	resp, err := c.inner.Get(url)
	if err != nil {
		return Response{}, err
	}
	return Response{inner: resp}, nil
}

func CloseIdleConnections(c Client) {
	c.inner.CloseIdleConnections()
}
`
