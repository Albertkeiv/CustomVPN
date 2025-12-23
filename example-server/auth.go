package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// AuthRequest represents the auth request body
type AuthRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// AuthResponse represents the auth response
type AuthResponse struct {
	AuthToken string `json:"authToken"`
}

// authHandler handles POST /auth
func authHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode auth request: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Check credentials
	user, exists := users[req.Login]
	if !exists || user.Password != req.Password {
		log.Printf("Auth failed for login: %s", req.Login)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`"Auth Failed"`))
		return
	}

	// Generate token
	token, err := generateToken()
	if err != nil {
		log.Printf("Failed to generate token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store token
	authToken := &AuthToken{
		Value:     token,
		UserLogin: req.Login,
		IssuedAt:  time.Now(),
		ExpiresAt: nil, // long-lived
	}
	tokens[token] = authToken

	log.Printf("Auth successful for login: %s, token: %s", req.Login, token)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(AuthResponse{AuthToken: token})
}

// generateToken generates a random token
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}