package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"spark/internal/auth"
	"spark/internal/auth/oauth"
	"spark/internal/config"
)

func newLoginCmd() *cobra.Command {
	var provider string
	var account string
	var apiKey string
	var useDevice bool
	var useBrowser bool
	var noBrowser bool
	var port int

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to an upstream AI provider using OAuth or Device Flow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(provider) == "" {
				provider = auth.ProviderClaude
			}
			spec, err := auth.SpecFor(provider)
			if err != nil {
				return err
			}

			store, err := auth.DefaultStore()
			if err != nil {
				return fmt.Errorf("open auth store: %w", err)
			}

			ctx := context.Background()

			var authRecord *auth.Auth
			if strings.TrimSpace(apiKey) != "" {
				key := strings.Trim(strings.TrimSpace(apiKey), "\"'` \t\r\n")
				authRecord = &auth.Auth{
					Provider:    spec.Provider,
					Kind:        auth.KindOAuth,
					AccessToken: key,
					Account:     strings.TrimSpace(account),
					CreatedAt:   time.Now(),
				}
				if err := store.Save(authRecord); err != nil {
					return fmt.Errorf("save auth credentials: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Successfully saved credentials for %s (%s)!\n", spec.Label, spec.Provider)
				if cfg, err := config.Load(); err == nil && cfg != nil {
					profName, created := cfg.EnsureProfileForAuthProvider(spec.Provider)
					if created {
						_ = config.Save(cfg)
						fmt.Fprintf(cmd.OutOrStdout(), "  Created profile: %s (bound to %s)\n", profName, spec.Label)
					}
				}
				if authRecord.Account != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  Account: %s\n", authRecord.Account)
					fmt.Fprintf(cmd.OutOrStdout(), "  AuthRef: %s\n", auth.FormatAuthRef(spec.Provider, authRecord.Account))
				}
				return nil
			}

			if useDevice && useBrowser {
				return fmt.Errorf("cannot specify both --device and --browser")
			}

			preferDevice := false
			if useDevice {
				preferDevice = true
			} else if useBrowser {
				preferDevice = false
			} else {
				// Default to device flow only for providers that don't support browser OAuth (e.g. Kimi)
				if spec.SupportsDeviceFlow && spec.AuthEndpoint == "" {
					preferDevice = true
				}
			}

			if preferDevice {
				if !spec.SupportsDeviceFlow {
					return fmt.Errorf("provider %s does not support device flow", spec.Provider)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Starting Device Authorization flow for %s (%s)...\n", spec.Label, spec.Provider)
				if spec.AuthEndpoint != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "(Note: You can also use -b / --browser to log in via browser OAuth)\n")
				}
				if !noBrowser {
					_ = oauth.OpenBrowser(spec.DeviceVerificationEndpoint)
				}
				authRecord, err = oauth.DeviceFlow(ctx, spec, func(res oauth.DeviceResult) error {
					fmt.Fprintf(cmd.OutOrStdout(), "\n! Please visit the following URL to authorize Spark:\n")
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n\n", res.VerificationURI)
					fmt.Fprintf(cmd.OutOrStdout(), "  Enter code: %s\n\n", res.UserCode)
					fmt.Fprintf(cmd.OutOrStdout(), "Waiting for authorization in browser...\n")
					return nil
				})
			} else {
				if spec.AuthEndpoint == "" {
					return fmt.Errorf("provider %s does not support browser OAuth; please use -d / --device", spec.Provider)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Starting browser OAuth flow for %s (%s)...\n", spec.Label, spec.Provider)
				if spec.SupportsDeviceFlow {
					fmt.Fprintf(cmd.OutOrStdout(), "(Note: You can also use -d / --device to log in via Device Code)\n")
				}
				manualCh := make(chan string, 1)
				go func() {
					scanner := bufio.NewScanner(cmd.InOrStdin())
					if scanner.Scan() {
						line := strings.Trim(strings.TrimSpace(scanner.Text()), "\"'` \t\r\n")
						if line != "" {
							manualCh <- line
						}
					}
				}()

				authRecord, err = oauth.BrowserFlow(ctx, spec, oauth.BrowserOptions{
					NoBrowser:         noBrowser,
					CallbackPort:      port,
					ManualCallbackURL: manualCh,
					OnAuthURL: func(u string) {
						fmt.Fprintf(cmd.OutOrStdout(), "\n! Open the following URL in your browser to authorize %s:\n  %s\n\nWaiting for authentication callback...\n(If automatic callback fails or you are on a remote server, paste the API key or callback URL here and press Enter):\n", spec.Label, u)
					},
				})
			}

			if err != nil {
				if preferDevice && spec.AuthEndpoint != "" {
					return fmt.Errorf("login failed: %w\nTip: If device code authorization is disabled in security settings, try: spark auth login -p %s --browser", err, spec.Provider)
				}
				return fmt.Errorf("login failed: %w", err)
			}

			if strings.TrimSpace(account) != "" {
				authRecord.Account = strings.TrimSpace(account)
			}

			if err := store.Save(authRecord); err != nil {
				return fmt.Errorf("save auth credentials: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Successfully logged in to %s (%s)!\n", spec.Label, spec.Provider)
			if cfg, err := config.Load(); err == nil && cfg != nil {
				profName, created := cfg.EnsureProfileForAuthProvider(spec.Provider)
				if created {
					_ = config.Save(cfg)
					fmt.Fprintf(cmd.OutOrStdout(), "  Created profile: %s (bound to %s)\n", profName, spec.Label)
				}
			}
			if authRecord.Account != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Account: %s\n", authRecord.Account)
				fmt.Fprintf(cmd.OutOrStdout(), "  AuthRef: %s\n", auth.FormatAuthRef(spec.Provider, authRecord.Account))
			}
			if !authRecord.ExpiresAt.IsZero() {
				fmt.Fprintf(cmd.OutOrStdout(), "  Expires: %s (%s)\n", authRecord.ExpiresAt.Format(time.RFC3339), time.Until(authRecord.ExpiresAt).Round(time.Minute))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&provider, "provider", "p", auth.ProviderClaude, "Provider to log in to (claude, codex, gemini, grok, kimi, commandcode)")
	cmd.Flags().StringVarP(&account, "account", "a", "", "Account identifier for multi-account support (e.g. work, personal)")
	cmd.Flags().StringVarP(&apiKey, "key", "k", "", "Direct API key / token to save (skip browser login)")
	cmd.Flags().BoolVarP(&useDevice, "device", "d", false, "Use Device Authorization flow (RFC 8628)")
	cmd.Flags().BoolVarP(&useBrowser, "browser", "b", false, "Force browser OAuth flow instead of device flow")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not attempt to open a browser automatically")
	cmd.Flags().IntVar(&port, "port", 0, "Loopback callback port override")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	var provider string
	var account string
	var all bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out and remove stored credentials for an AI provider",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := auth.DefaultStore()
			if err != nil {
				return fmt.Errorf("open auth store: %w", err)
			}

			if all {
				records, err := store.List()
				if err != nil {
					return err
				}
				for _, r := range records {
					_ = store.Delete(r.Provider)
					fmt.Fprintf(cmd.OutOrStdout(), "✓ Logged out from %s\n", r.Provider)
				}
				return nil
			}

			if strings.TrimSpace(provider) == "" {
				return fmt.Errorf("please specify a provider with --provider <name> or use --all")
			}

			targetProvider := strings.TrimSpace(provider)
			if strings.Contains(targetProvider, ":") {
				// Already ref format like "claude:work"
			} else {
				if spec, err := auth.SpecFor(targetProvider); err == nil {
					targetProvider = spec.Provider
				}
				if strings.TrimSpace(account) != "" {
					targetProvider = auth.FormatAuthRef(targetProvider, account)
				}
			}

			if err := store.Delete(targetProvider); err != nil {
				return fmt.Errorf("logout %s: %w", targetProvider, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Successfully logged out from %s\n", targetProvider)
			return nil
		},
	}

	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Provider to log out from (claude, codex, gemini, grok, kimi, commandcode)")
	cmd.Flags().StringVar(&account, "account", "", "Account to log out from when multiple accounts are configured")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Log out from all providers")
	return cmd
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage upstream OAuth/Device authentication credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthStatus(cmd)
		},
	}

	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newLoginCmd())
	cmd.AddCommand(newLogoutCmd())
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List active login credentials and expiry status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthStatus(cmd)
		},
	}
}

func runAuthStatus(cmd *cobra.Command) error {
	store, err := auth.DefaultStore()
	if err != nil {
		return fmt.Errorf("open auth store: %w", err)
	}

	records, err := store.List()
	if err != nil {
		return fmt.Errorf("list auth records: %w", err)
	}

	if len(records) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No active OAuth/Device login credentials found.")
		fmt.Fprintln(cmd.OutOrStdout(), "Run `spark login --provider <claude|codex|gemini|grok>` to log in.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tACCOUNT\tAUTH_REF\tKIND\tSTATUS\tEXPIRES")
	for _, r := range records {
		status := "Active"
		if r.Expired(0) {
			if r.RefreshToken != "" {
				status = "Expired (Refreshable)"
			} else {
				status = "Expired"
			}
		}

		expires := "-"
		if !r.ExpiresAt.IsZero() {
			if r.Expired(0) {
				expires = fmt.Sprintf("Expired %s ago", time.Since(r.ExpiresAt).Round(time.Minute))
			} else {
				expires = fmt.Sprintf("In %s", time.Until(r.ExpiresAt).Round(time.Minute))
			}
		}

		account := r.Account
		if account == "" {
			account = "-"
		}
		authRef := auth.FormatAuthRef(r.Provider, r.Account)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Provider, account, authRef, r.Kind, status, expires)
	}
	_ = w.Flush()
	_ = os.Stdout
	return nil
}
