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

func adminServiceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "service",
		Short: "Manage services (admin)",
	}
	c.AddCommand(adminServiceCreateCmd())
	c.AddCommand(adminServiceListCmd())
	c.AddCommand(adminServiceDeleteCmd())
	return c
}

func adminServiceCreateCmd() *cobra.Command {
	var (
		projectSlug string
		siteSlug    string
		serviceSlug string
		targetAddr  string
		targetPort  uint16
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectSlug == "" || siteSlug == "" || serviceSlug == "" || targetAddr == "" || targetPort == 0 {
				return fmt.Errorf("admin service create: --project, --site, --slug, --target-addr, --target-port required")
			}
			for k, v := range map[string]string{"project": projectSlug, "site": siteSlug, "service": serviceSlug} {
				if err := resource.ValidateSlug(v); err != nil {
					return fmt.Errorf("admin service create: %s: %w", k, err)
				}
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()

			p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
			if err != nil {
				return fmt.Errorf("admin service create: %w", err)
			}
			site, err := dao.NewSiteDAO(db).FindByName(context.Background(), p.ID, siteSlug)
			if err != nil {
				return fmt.Errorf("admin service create: %w", err)
			}
			store := dao.NewServiceDAO(db)
			relayPort, err := store.AllocateRelayPort(context.Background(), p)
			if err != nil {
				return fmt.Errorf("admin service create: %w", err)
			}
			svc, err := store.Create(context.Background(), &model.Service{
				SiteID: site.ID, Name: serviceSlug,
				TargetAddr: targetAddr, TargetPort: targetPort,
				Proto: model.ProtoTCP, RelayPort: &relayPort,
			})
			if err != nil {
				return fmt.Errorf("admin service create: %w", err)
			}
			cmd.Printf("created service %q (id=%d, relay_port=%d)\n", svc.Name, svc.ID, relayPort)
			return nil
		},
	}
	c.Flags().StringVar(&projectSlug, "project", "", "project slug")
	c.Flags().StringVar(&siteSlug, "site", "", "site slug")
	c.Flags().StringVar(&serviceSlug, "slug", "", "service slug")
	c.Flags().StringVar(&targetAddr, "target-addr", "", "target IP (127.0.0.1 or LAN)")
	c.Flags().Uint16Var(&targetPort, "target-port", 0, "target TCP port")
	return c
}

func adminServiceListCmd() *cobra.Command {
	var (
		projectSlug string
		siteSlug    string
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "List services in a site",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectSlug == "" || siteSlug == "" {
				return fmt.Errorf("admin service list: --project and --site required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
			if err != nil {
				return fmt.Errorf("admin service list: %w", err)
			}
			site, err := dao.NewSiteDAO(db).FindByName(context.Background(), p.ID, siteSlug)
			if err != nil {
				return fmt.Errorf("admin service list: %w", err)
			}
			rows, err := dao.NewServiceDAO(db).ListBySite(context.Background(), site.ID, 1000, "")
			if err != nil {
				return fmt.Errorf("admin service list: %w", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		},
	}
	c.Flags().StringVar(&projectSlug, "project", "", "project slug")
	c.Flags().StringVar(&siteSlug, "site", "", "site slug")
	return c
}

func adminServiceDeleteCmd() *cobra.Command {
	var (
		projectSlug string
		siteSlug    string
		serviceSlug string
	)
	c := &cobra.Command{
		Use:   "delete",
		Short: "Delete a service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectSlug == "" || siteSlug == "" || serviceSlug == "" {
				return fmt.Errorf("admin service delete: --project, --site, --slug required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
			if err != nil {
				return fmt.Errorf("admin service delete: %w", err)
			}
			site, err := dao.NewSiteDAO(db).FindByName(context.Background(), p.ID, siteSlug)
			if err != nil {
				return fmt.Errorf("admin service delete: %w", err)
			}
			store := dao.NewServiceDAO(db)
			svc, err := store.FindByName(context.Background(), site.ID, serviceSlug)
			if err != nil {
				return fmt.Errorf("admin service delete: %w", err)
			}
			if err := store.Delete(context.Background(), svc.ID); err != nil {
				return fmt.Errorf("admin service delete: %w", err)
			}
			cmd.Printf("deleted service %q\n", svc.Name)
			return nil
		},
	}
	c.Flags().StringVar(&projectSlug, "project", "", "project slug")
	c.Flags().StringVar(&siteSlug, "site", "", "site slug")
	c.Flags().StringVar(&serviceSlug, "slug", "", "service slug")
	return c
}
