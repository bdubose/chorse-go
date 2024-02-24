package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"

	jwt "github.com/golang-jwt/jwt/v4"
	"golang.org/x/oauth2"
)

func WriteJson(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status) // headers must be set before calling this method
	return json.NewEncoder(w).Encode(v)
}

func WriteHtml(w http.ResponseWriter, status int, v string) {
	w.WriteHeader(status)
	fmt.Fprint(w, v)
}

type apiFunc func(http.ResponseWriter, *http.Request) error

type ApiError struct {
	Error string
}

func withJwtAuth(handlerFunc http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Calling JWTAuth middleware")
		tokenStr := r.Header.Get("x-jwt-token")
		token, err := validateJwt(tokenStr)
		if err != nil {
			WriteJson(w, http.StatusForbidden, &ApiError{Error: "invalid token"})
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			fmt.Printf("Found AccountNumber: %v\n", claims["accountNumber"])
		}
		handlerFunc(w, r)
	}
}

func validateJwt(tokenStr string) (*jwt.Token, error) {
	secret := os.Getenv("JWT_SECRET")
	return jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return []byte(secret), nil
	})
}

func createJwt(account *Account) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"expiresAt":     15_000,
		"accountNumber": account.Number,
	})

	return token.SignedString([]byte(secret))
}

func makeHttpHandleFunc(f apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			WriteHtml(w, http.StatusInternalServerError, fmt.Sprintf("<h1>Error</h1><p>%v</p>", err))
		}
	}
}

type ApiServer struct {
	listenAddr string
	store      Storage
	auth       *oauth2.Config
}

func NewApiService(listenAddr string, store Storage, auth *oauth2.Config) *ApiServer {
	return &ApiServer{
		listenAddr: listenAddr,
		store:      store,
		auth:       auth,
	}
}

func (s *ApiServer) Run() {
	router := http.NewServeMux()

	router.Handle("/", http.FileServer(http.Dir("./static")))

	router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.auth.AuthCodeURL("randomstate"), http.StatusTemporaryRedirect)
	})
	router.HandleFunc("/auth/callback", s.handleAuthCallback)

	router.HandleFunc("/view/{viewName}", makeHttpHandleFunc(s.handleView))

	router.HandleFunc("/account", makeHttpHandleFunc(s.handleAccounts))
	router.HandleFunc("/account/{id}", withJwtAuth(makeHttpHandleFunc(s.handleOneAccount)))

	router.HandleFunc("/transfer", makeHttpHandleFunc(s.handleTransfer))

	log.Printf("Server running on port: %v\n", s.listenAddr)

	http.ListenAndServe(s.listenAddr, router)
}

/*
/avatars/user_id/user_avatar.png
https://cdn.discordapp.com/avatars/485103041738047489/13a45106234fa19fd7b22795df2b6833.png
*/

func (s *ApiServer) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("state") != "randomstate" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("State does not match."))
		return
	}

	token, err := s.auth.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		quickErr(w, err)
		return
	}

	res, err := s.auth.Client(r.Context(), token).Get("https://discord.com/api/users/@me")
	if err != nil || res.StatusCode != 200 {
		w.WriteHeader(http.StatusInternalServerError)
		if err != nil {
			w.Write([]byte(err.Error()))
		} else {
			w.Write([]byte(res.Status))
		}
		return
	}
	defer res.Body.Close()

	user := &DiscordUser{}
	if json.NewDecoder(res.Body).Decode(&user); err != nil {
		quickErr(w, err)
		return
	}

	exists, err := s.store.DiscordUserExists(r.Context(), user.Id)
	if err != nil {
		quickErr(w, err)
	}

	if !exists {
		s.store.CreateDiscordUser(r.Context(), user)
	}
}

func quickErr(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(err.Error()))
}

func (s *ApiServer) handleView(w http.ResponseWriter, r *http.Request) error {
	var contentStr string
	viewFileName := "./view/" + r.PathValue("viewName") + ".gohtml"
	mainContent, err := os.ReadFile(viewFileName)
	if os.IsNotExist(err) {
		contentStr = "<p>👀What you're looking for cannot be found.</p>"
	} else {
		contentStr = string(mainContent)
	}

	// if this is not an htmx request, we need to provide the rest of the layout
	if r.Header.Get("Hx-Request") == "" {
		return handleWholeView(w, mainContent)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, contentStr)
	return nil
}

func handleWholeView(w http.ResponseWriter, mainContent []byte) error {
	t, err := template.New("index.gohtml").ParseFiles("./templ/index.gohtml")
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusOK)
	return t.Execute(w, template.HTML(mainContent))
}

func (s *ApiServer) handleAccounts(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		return s.handleGetAllAccounts(w, r)
	case http.MethodPost:
		return s.handleCreateAccount(w, r)
	}
	return fmt.Errorf("method not allowed: %s", r.Method)
}

func (s *ApiServer) handleOneAccount(w http.ResponseWriter, r *http.Request) error {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return fmt.Errorf("invalid id given: %s", idStr)
	}
	switch r.Method {
	case http.MethodGet:
		return s.handleGetAccount(w, r, id)
	case http.MethodDelete:
		return s.handleDeleteAccount(w, r, id)
	}
	return fmt.Errorf("method not allowed: %s", r.Method)
}

func (s *ApiServer) handleGetAllAccounts(w http.ResponseWriter, r *http.Request) error {
	accounts, err := s.store.GetAccounts(r.Context())
	if err != nil {
		return err
	}
	return WriteJson(w, http.StatusOK, &accounts)
}

func (s *ApiServer) handleCreateAccount(w http.ResponseWriter, r *http.Request) error {
	accRequest := &CreateAccountRequest{}
	if err := json.NewDecoder(r.Body).Decode(&accRequest); err != nil {
		return err
	}

	account := NewAccount(accRequest.FirstName, accRequest.LastName)
	dbAccount, err := s.store.CreateAccount(r.Context(), account)
	if err != nil {
		return err
	}

	tokenStr, err := createJwt(account)
	if err != nil {
		return err
	}

	fmt.Printf("JWT Token: %s\n", tokenStr)

	return WriteJson(w, http.StatusOK, dbAccount)
}

func (s *ApiServer) handleGetAccount(w http.ResponseWriter, r *http.Request, id int) error {
	account, err := s.store.GetAccountById(r.Context(), id)
	if err != nil {
		return err
	}
	if account == nil {
		return WriteJson(w, http.StatusNotFound, nil)
	}

	return WriteJson(w, http.StatusOK, account)
}

func (s *ApiServer) handleDeleteAccount(w http.ResponseWriter, r *http.Request, id int) error {
	if err := s.store.DeleteAccount(r.Context(), id); err != nil {
		return err
	}
	return WriteJson(w, http.StatusOK, nil)
}

func (s *ApiServer) handleTransfer(w http.ResponseWriter, r *http.Request) error {
	transferRequest := &TransferRequest{}
	if err := json.NewDecoder(r.Body).Decode(&transferRequest); err != nil {
		return err
	}
	return nil
}
