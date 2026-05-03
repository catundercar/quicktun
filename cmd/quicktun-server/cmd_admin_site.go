package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

func adminSiteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "site",
		Short: "Manage sites (admin)",
	}
	c.AddCommand(adminSiteCreateCmd())
	c.AddCommand(adminSiteListCmd())
	c.AddCommand(adminSiteDeleteCmd())
	return c
}

func adminSiteCreateCmd() *cobra.Command {
	var (
		projectSlug string
		siteSlug    string
		displayName string
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a site under a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectSlug == "" || siteSlug == "" || displayName == "" {
				return fmt.Errorf("admin site create: --project, --slug, --display-name required")
			}
			if err := resource.ValidateSlug(projectSlug); err != nil {
				return fmt.Errorf("admin site create: project: %w", err)
			}
			if err := resource.ValidateSlug(siteSlug); err != nil {
				return fmt.Errorf("admin site create: site: %w", err)
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
			if err != nil {
				return fmt.Errorf("admin site create: %w", err)
			}
			s, err := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
				ProjectID: p.ID, Name: siteSlug, Mode: model.SiteModeEndpoint,
				Status: model.SiteStatusPending,
			})
			if err != nil {
				return fmt.Errorf("admin site create: %w", err)
			}
			cmd.Printf("created site %q in project %q (id=%d)\n", s.Name, p.Slug, s.ID)
			return nil
		},
	}
	c.Flags().StringVar(&projectSlug, "project", "", "project slug")
	c.Flags().StringVar(&siteSlug, "slug", "", "site slug")
	c.Flags().StringVar(&displayName, "display-name", "", "display name (currently informational)")
	return c
}

func adminSiteListCmd() *cobra.Command {
	var projectSlug string
	c := &cobra.Command{
		Use:   "list",
		Short: "List sites under a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectSlug == "" {
				return fmt.Errorf("admin site list: --project required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
			if err != nil {
				return fmt.Errorf("admin site list: %w", err)
			}
			rows, err := dao.NewSiteDAO(db).ListByProject(context.Background(), p.ID, 1000, "")
			if err != nil {
				return fmt.Errorf("admin site list: %w", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		},
	}
	c.Flags().StringVar(&projectSlug, "project", "", "project slug")
	return c
}

func adminSiteDeleteCmd() *cobra.Command {
	var (
		projectSlug string
		siteSlug    string
		force       bool
	)
	c := &cobra.Command{
		Use:   "delete",
		Short: "Delete a site",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectSlug == "" || siteSlug == "" {
				return fmt.Errorf("admin site delete: --project and --slug required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
			if err != nil {
				return fmt.Errorf("admin site delete: %w", err)
			}
			store := dao.NewSiteDAO(db)
			site, err := store.FindByName(context.Background(), p.ID, siteSlug)
			if err != nil {
				return fmt.Errorf("admin site delete: %w", err)
			}
			if !force {
				n, err := store.CountServices(context.Background(), site.ID)
				if err != nil {
					return fmt.Errorf("admin site delete: %w", err)
				}
				if n > 0 {
					return fmt.Errorf("admin site delete: site has %d services; pass --force", n)
				}
			}
			if err := store.Delete(context.Background(), site.ID); err != nil {
				return fmt.Errorf("admin site delete: %w", err)
			}
			cmd.Printf("deleted site %q in project %q\n", site.Name, p.Slug)
			return nil
		},
	}
	c.Flags().StringVar(&projectSlug, "project", "", "project slug")
	c.Flags().StringVar(&siteSlug, "slug", "", "site slug")
	c.Flags().BoolVar(&force, "force", false, "cascade-delete services")
	return c
}
