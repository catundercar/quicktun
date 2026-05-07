package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/model"
)

// adminListOperatorsCmd lists all operator rows. Used primarily by the
// install-server.sh bootstrap to detect "is there already an admin?" before
// prompting for one. Output is one operator per line, tab-separated:
//
//	<id>\t<email>\t<is_admin>
//
// Soft-deleted operators are excluded by gorm's default scope (they have a
// non-NULL deleted_at).
func adminListOperatorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-operators",
		Short: "List all operators (id, email, is_admin) — tab-separated, one per line",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()

			var ops []model.Operator
			if err := db.WithContext(context.Background()).Find(&ops).Error; err != nil {
				return fmt.Errorf("admin: list operators: %w", err)
			}
			for _, o := range ops {
				cmd.Printf("%d\t%s\t%v\n", o.ID, o.Email, o.IsAdmin)
			}
			return nil
		},
	}
}
