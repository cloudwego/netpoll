module github.com/cloudwego/netpoll

go 1.21

require (
	github.com/bytedance/gopkg v0.0.0-20220413063733-65bf48ffb3a7
	github.com/mdlayher/socket v0.5.0
	github.com/mdlayher/vsock v1.2.1
	golang.org/x/sys v0.11.0
)

require (
	golang.org/x/net v0.14.0 // indirect
	golang.org/x/sync v0.3.0 // indirect
)

replace github.com/mdlayher/vsock => github.com/joway/vsock v0.0.0-20240129032524-548b80ee60e4

replace github.com/mdlayher/socket => github.com/joway/socket v0.0.0-20240129032302-36ee54dae5dc
