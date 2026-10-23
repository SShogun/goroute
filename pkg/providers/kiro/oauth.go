package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	DefaultSocialClientID   = "kiro-cli"
	SocialDeviceAuthURL     = "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/device/authorization"
	SocialDevicePollURL     = "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/device/poll"
)

// DeviceAuthResponse holds the user verification code and link
type DeviceAuthResponse struct {
	AuthURL     string `json:"authUrl"`
	DeviceCode  string `json:"deviceCode"`
	UserCode    string `json:"userCode"`
	ExpiresIn   int    `json:"expiresIn"`
	Interval    int    `json:"interval"`
	Provider    string `json:"provider"`
}

// DevicePollResult holds the polling status from AWS Kiro
type DevicePollResult struct {
	Success      bool   `json:"success"`
	Pending      bool   `json:"pending"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ProfileARN   string `json:"profileArn,omitempty"`
	Email        string `json:"email,omitempty"`
	Error        string `json:"error,omitempty"`
}

// StartSocialDeviceAuth initiates the device authorization flow with Google or GitHub
func StartSocialDeviceAuth(ctx context.Context, provider string) (*DeviceAuthResponse, error) {
	loginProvider := "Google"
	if provider == "github" {
		loginProvider = "Github"
	}

	payload := map[string]string{
		"clientId":      DefaultSocialClientID,
		"loginProvider": loginProvider,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", SocialDeviceAuthURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device authorize request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream device auth error (%d): %s", resp.StatusCode, string(raw))
	}

	var rawData struct {
		DeviceCode             string `json:"deviceCode"`
		UserCode               string `json:"userCode"`
		VerificationURIComplete string `json:"verificationUriComplete"`
		ExpiresInMilliseconds  int    `json:"expiresInMilliseconds"`
		IntervalInMilliseconds int    `json:"intervalInMilliseconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawData); err != nil {
		return nil, fmt.Errorf("decode device auth response: %w", err)
	}

	return &DeviceAuthResponse{
		AuthURL:    rawData.VerificationURIComplete,
		DeviceCode: rawData.DeviceCode,
		UserCode:   rawData.UserCode,
		ExpiresIn:  rawData.ExpiresInMilliseconds / 1000,
		Interval:   rawData.IntervalInMilliseconds / 1000,
		Provider:   provider,
	}, nil
}

// PollSocialDeviceAuth polls AWS Kiro for credentials
func PollSocialDeviceAuth(ctx context.Context, deviceCode string) (*DevicePollResult, error) {
	payload := map[string]string{
		"clientId":   DefaultSocialClientID,
		"deviceCode": deviceCode,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", SocialDevicePollURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll request failed: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Status       string `json:"status"`
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileARN   string `json:"profileArn"`
		Error        string `json:"error"`
		Message      string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}

	if data.Status == "authorization_pending" || data.Error == "authorization_pending" {
		return &DevicePollResult{
			Success: false,
			Pending: true,
		}, nil
	}

	if data.AccessToken != "" && data.RefreshToken != "" {
		arn := data.ProfileARN
		if arn == "" {
			arn = "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK"
		}
		return &DevicePollResult{
			Success:      true,
			Pending:      false,
			AccessToken:  data.AccessToken,
			RefreshToken: data.RefreshToken,
			ProfileARN:   arn,
		}, nil
	}

	errMsg := data.Error
	if errMsg == "" {
		errMsg = data.Message
	}
	if errMsg == "" {
		errMsg = "authorization incomplete"
	}

	return &DevicePollResult{
		Success: false,
		Pending: false,
		Error:   errMsg,
	}, nil
}
