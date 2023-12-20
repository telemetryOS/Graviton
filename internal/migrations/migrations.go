package migrations

import (
	"context"

	"github.com/telemetrytv/graviton-cli/internal/driver"
)

type Migration struct {
	*driver.MigrationMetadata
	up   func(context.Context, any) error
	down func(context.Context, any) error
}

func (m *Migration) Up(ctx context.Context, handle any) error {
	return nil
}

func (m *Migration) Down(ctx context.Context, handle any) error {
	return nil
}
