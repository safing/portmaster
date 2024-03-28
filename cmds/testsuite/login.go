package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/access/account"
)

var (
	loginCmd = &cobra.Command{
		Use:   "login",
		Short: "Test login and token issuing",
		RunE:  runTestCommand(testLogin),
	}

	loginUsername string
	loginPassword string
	loginDeviceID string
)

func init() {
	rootCmd.AddCommand(loginCmd)

	// Add flags for login options.
	flags := loginCmd.Flags()
	flags.StringVar(&loginUsername, "username", "", "set username to use for the login test")
	flags.StringVar(&loginPassword, "password", "", "set password to use for the login test")
	flags.StringVar(&loginDeviceID, "device-id", "", "set device ID to use for the login test")

	// Mark all as required.
	_ = loginCmd.MarkFlagRequired("username")
	_ = loginCmd.MarkFlagRequired("password")
	_ = loginCmd.MarkFlagRequired("device-id")
}

func testLogin(cmd *cobra.Command, args []string) (err error) {
	// Init token zones.
	err = access.InitializeZones()
	if err != nil {
		return fmt.Errorf("failed to initialize token zones: %w", err)
	}

	// Set initial user object in order to set the device ID that should be used for login.
	initialUser := &access.UserRecord{
		User: &account.User{
			Username: loginUsername,
			Device: &account.Device{
				ID: loginDeviceID,
			},
		},
	}
	err = initialUser.Save()
	if err != nil {
		return fmt.Errorf("failed to save initial user with device ID: %w", err)
	}

	// Login.
	_, _, err = access.Login(loginUsername, loginPassword)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Check user.
	user, err := access.GetUser()
	if err != nil {
		return fmt.Errorf("failed to get user after login: %w", err)
	}
	if verbose {
		log.Printf("user (from login): %+v", user.User)
		log.Printf("device (from login): %+v", user.User.Device)
	}

	// Check if the device ID is unchanged.
	if user.Device.ID != loginDeviceID {
		return errors.New("device ID changed")
	}

	// Check Auth Token.
	authToken, err := access.GetAuthToken()
	if err != nil {
		return fmt.Errorf("failed to get auth token after login: %w", err)
	}
	if verbose {
		log.Printf("auth token (from login): %+v", authToken.Token)
	}
	firstAuthToken := authToken.Token.Token

	// Update User.
	_, _, err = access.UpdateUser()
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Check if we received a new Auth Token.
	authToken, err = access.GetAuthToken()
	if err != nil {
		return fmt.Errorf("failed to get auth token after user update: %w", err)
	}
	if verbose {
		log.Printf("auth token (from update): %+v", authToken.Token)
	}
	if authToken.Token.Token == firstAuthToken {
		return errors.New("auth token did not change after update")
	}

	// Get Tokens.
	err = access.UpdateTokens()
	if err != nil {
		return fmt.Errorf("failed to update tokens: %w", err)
	}
	regular, fallback := access.GetTokenAmount(access.ExpandAndConnectZones)
	if verbose {
		log.Printf("received tokens: %d regular, %d fallback", regular, fallback)
	}
	if regular == 0 || fallback == 0 {
		return fmt.Errorf("not enough tokens after fetching: %d regular, %d fallback", regular, fallback)
	}

	return nil
}
