package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"

	"github.com/tulip/quicktun/internal/dao"
)

func adminCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands",
	}
	c.AddCommand(adminCreateOperatorCmd())
	c.AddCommand(adminProjectCmd())
	return c
}

func adminCreateOperatorCmd() *cobra.Command {
	var (
		email    string
		password string
		isAdmin  bool
	)
	c := &cobra.Command{
		Use:   "create-operator",
		Short: "Create a new operator account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" || password == "" {
				return fmt.Errorf("admin: --email and --password are required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()

			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("admin: hash password: %w", err)
			}
			op, err := dao.NewOperatorDAO(db).Create(context.Background(), email, string(hash), isAdmin)
			if err != nil {
				return fmt.Errorf("admin: create operator: %w", err)
			}
			cmd.Printf("created operator %q (id=%d, admin=%v)\n", op.Email, op.ID, op.IsAdmin)
			return nil
		},
	}
	c.Flags().StringVar(&email, "email", "", "operator email (required)")
	c.Flags().StringVar(&password, "password", "", "operator password (required)")
	c.Flags().BoolVar(&isAdmin, "admin", false, "grant admin role")
	return c
}
