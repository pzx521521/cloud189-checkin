// Package web 内嵌顶层静态前端资源，供 cmd/api 通过 http.FileServer 对外服务。
package web

import "embed"

//go:embed index.html app.js
var FS embed.FS
