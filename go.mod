module github.com/cloudwego/netpoll

go 1.15

require (
	github.com/bytedance/gopkg v0.0.0-20220413063733-65bf48ffb3a7
	github.com/mdlayher/socket v0.5.0
	github.com/mdlayher/vsock v1.2.1
	golang.org/x/sys v0.11.0
)

replace github.com/mdlayher/vsock => github.com/joway/vsock v0.0.0-20240124095658-abb82a7e66c5

replace github.com/mdlayher/socket => github.com/joway/socket v0.0.0-20240124092902-ce5d567234fb
