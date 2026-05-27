/*
Package gateway exposes the Mission Control plugin ingress layer.

It owns the HTTP API for listing plugins, invoking operations, and proxying
plugin UI/HTTP handlers, plus the host-side gRPC service that plugins call
back into for config, connection, logging, and cross-plugin invocation.
Gateway performs request validation, RBAC, selector checks, token minting,
and audit recording, while delegating plugin lifecycle/connectivity to the
runtime machinery layer.
*/

package gateway
