module github.com/ryeng/agentai-backend-monorepo/backend/services/gateway

go 1.23.0

toolchain go1.24.4

require (
	github.com/MicahParks/keyfunc/v2 v2.1.0
	github.com/go-chi/chi/v5 v5.2.5
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/ryeng/agentai-backend-monorepo/backend/contracts v0.0.0
	github.com/ryeng/agentai-backend-monorepo/backend/libs v0.0.0
	google.golang.org/grpc v1.74.0
)

require (
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250528174236-200df99c418a // indirect
	google.golang.org/protobuf v1.36.7 // indirect
)

replace github.com/ryeng/agentai-backend-monorepo/backend/contracts => ../../contracts

replace github.com/ryeng/agentai-backend-monorepo/backend/libs => ../../libs
