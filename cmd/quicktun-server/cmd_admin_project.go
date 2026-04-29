package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

func adminProjectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "project",
		Short: "Manage projects (admin)",
	}
	c.AddCommand(adminProjectCreateCmd())
	c.AddCommand(adminProjectListCmd())
	c.AddCommand(adminProjectDeleteCmd())
	return c
}

func adminProjectCreateCmd() *cobra.Command {
	var (
		slug           string
		displayName    string
		relayPortRange string
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" || displayName == "" || relayPortRange == "" {
				return fmt.Errorf("admin project create: --slug, --display-name, --relay-port-range required")
			}
			if err := resource.ValidateSlug(slug); err != nil {
				return fmt.Errorf("admin project create: %w", err)
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
				Slug:           slug,
				Name:           displayName,
				RelayPortRange: relayPortRange,
				DefaultMode:    model.SiteModeEndpoint,
				Backend:        model.BackendRathole,
				Status:         model.ProjectStatusActive,
			})
			if err != nil {
				return fmt.Errorf("admin project create: %w", err)
			}
			cmd.Printf("created project %q (id=%d)\n", p.Slug, p.ID)
			return nil
		},
	}
	c.Flags().StringVar(&slug, "slug", "", "URL-safe project slug")
	c.Flags().StringVar(&displayName, "display-name", "", "human-friendly name")
	c.Flags().StringVar(&relayPortRange, "relay-port-range", "", "e.g. \"20000-20999\"")
	return c
}

func adminProjectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			rows, err := dao.NewProjectDAO(db).List(context.Background(), 1000, "")
			if err != nil {
				return fmt.Errorf("admin project list: %w", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		},
	}
}

func adminProjectDeleteCmd() *cobra.Command {
	var (
		slug  string
		force bool
	)
	c := &cobra.Command{
		Use:   "delete",
		Short: "Delete (soft) a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" {
				return fmt.Errorf("admin project delete: --slug is required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			store := dao.NewProjectDAO(db)
			p, err := store.FindBySlug(context.Background(), slug)
			if err != nil {
				return fmt.Errorf("admin project delete: %w", err)
			}
			if !force {
				n, err := store.CountSites(context.Background(), p.ID)
				if err != nil {
					return fmt.Errorf("admin project delete: %w", err)
				}
				if n > 0 {
					return fmt.Errorf("admin project delete: project has %d sites; pass --force to cascade", n)
				}
			}
			if err := store.Delete(context.Background(), p.ID); err != nil {
				return fmt.Errorf("admin project delete: %w", err)
			}
			cmd.Printf("deleted project %q\n", slug)
			return nil
		},
	}
	c.Flags().StringVar(&slug, "slug", "", "project slug")
	c.Flags().BoolVar(&force, "force", false, "cascade-delete sites")
	return c
}

// openAdminDB is a helper used by every admin subcommand. It loads config from
// the persistent --config flag, opens the DB, and returns it.
func openAdminDB(cmd *cobra.Command) (*gorm.DB, error) {
	cfgPath, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("admin: %w", err)
	}
	db, err := dao.Open(cfg.Database.DSN, nil)
	if err != nil {
		return nil, fmt.Errorf("admin: %w", err)
	}
	return db, nil
}
