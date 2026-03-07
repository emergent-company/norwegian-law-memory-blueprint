module github.com/emergent-company/norwegian-law-memory-blueprint

go 1.24.0

require (
	github.com/emergent-company/emergent.memory/apps/server/pkg/sdk v0.0.0
	golang.org/x/net v0.49.0
)

replace github.com/emergent-company/emergent.memory/apps/server/pkg/sdk => /root/emergent.memory/apps/server/pkg/sdk
