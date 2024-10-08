package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	gorillaSessions "github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/stytchauth/stytch-go/v12/stytch/consumer/magiclinks"
	"github.com/stytchauth/stytch-go/v12/stytch/consumer/magiclinks/email"
	"github.com/stytchauth/stytch-go/v12/stytch/consumer/sessions"
	"github.com/stytchauth/stytch-go/v12/stytch/consumer/stytchapi"
	"github.com/stytchauth/stytch-go/v12/stytch/consumer/users"
)

var (
	store   = gorillaSessions.NewCookieStore([]byte("your-secret-key"))
	TESTING = false
)

type config struct {
	address      string
	fullAddress  string
	stytchClient *stytchapi.API
	whitelist    bool
}

// struct to hold the values to be passed to the html templates
type templateVariables struct {
	LoginOrCreateUserPath string
	LoggedOutPath         string
	EmailAddress          string
	RegisterPasskeyPath   string
	NavLinks              map[string]template.HTML
	Who                   string
	Products              []Product
}

type Product struct {
	Name      string
	Imagename string
}

func main() {
	// Load .env & set config
	c, err := initializeConfig()
	if err != nil {
		log.Fatal("error initializing config")
	}

	r := mux.NewRouter()
	fmt.Println("Navigate to", c.fullAddress, "to see the Hello Socks app!")

	// routes
	r.HandleFunc("/", c.homepage).Methods("GET")
	r.HandleFunc("/login_or_create_user", c.loginOrCreateUser).Methods("POST")
	r.HandleFunc("/authenticate", c.authenticate).Methods("GET")
	r.HandleFunc("/logout", c.logout).Methods("GET")
	r.HandleFunc("/men", c.menscollection).Methods("GET")
	r.HandleFunc("/women", c.womenscollection).Methods("GET")
	r.HandleFunc("/kids", c.kidscollection).Methods("GET")
	r.HandleFunc("/test", c.test).Methods("GET")

	// Declare the static file directory
	// this is to ensure our static assets & css are accessible & rendered
	staticFileDirectory := http.Dir("./assets/")
	staticFileHandler := http.StripPrefix("/assets/", http.FileServer(staticFileDirectory))
	r.PathPrefix("/assets/").Handler(staticFileHandler)

	log.Fatal(http.ListenAndServe(c.address, r))
}

func (c *config) test(w http.ResponseWriter, r *http.Request) {
	TESTING = true
}

// handles the homepage for Hello Socks
func (c *config) homepage(w http.ResponseWriter, r *http.Request) {
	user := c.getAuthenticatedUser(w, r)

	if user != nil {
		linksmap := make(map[string]template.HTML)
		linksmap["Women"] = template.HTML(`<a href="/women">Women</a>`)
		linksmap["Men"] = template.HTML(`<a href="/men">Men</a>`)
		linksmap["Kids"] = template.HTML(`<a href="/kids">Kids</a>`)
		parseAndExecuteTemplate(
			"templates/loggedIn.html",
			&templateVariables{LoggedOutPath: c.fullAddress + "/logout",
				EmailAddress: user.Emails[0].Email,
				NavLinks:     linksmap,
			},
			w,
		)
	}

	parseAndExecuteTemplate(
		"templates/loginOrSignUp.html",
		&templateVariables{LoginOrCreateUserPath: c.fullAddress + "/login_or_create_user", RegisterPasskeyPath: c.fullAddress + "/register_passkey"},
		w,
	)

}

// takes the email entered on the homepage and hits the stytch
// loginOrCreateUser endpoint to send the user a magic link
func (c *config) loginOrCreateUser(w http.ResponseWriter, r *http.Request) {

	//if we only allow pre-defined users access
	if c.whitelist {
		//check we have this user
		if c.findUser(r.FormValue("email")) {
			err := c.sendEmail(r.FormValue("email"))
			if err != nil {
				log.Print(err)
				parseAndExecuteTemplate("templates/error.html", nil, w)
			} else {
				parseAndExecuteTemplate("templates/emailSent.html", nil, w)
			}
		} else {
			log.Printf("could not find whitelist user %v", r.FormValue("email"))
			parseAndExecuteTemplate("templates/forbidden.html", nil, w)
		}
	} else { //anyone can register, registered people can log in
		err := c.sendEmail(r.FormValue("email"))
		if err != nil {
			log.Print(err)
			parseAndExecuteTemplate("templates/error.html", nil, w)
		} else {
			parseAndExecuteTemplate("templates/emailSent.html", nil, w)
		}
	}

}

func (c *config) sendEmail(emailaddr string) error {
	_, err := c.stytchClient.MagicLinks.Email.LoginOrCreate(
		context.Background(),
		&email.LoginOrCreateParams{
			Email: emailaddr,
		})
	if err != nil {
		return fmt.Errorf("something went wrong sending magic link: %s", err)
	}

	return nil
}

func (c *config) findUser(email string) bool {

	srchresp, err := c.stytchClient.Users.Search(context.Background(), &users.SearchParams{
		Limit: 1,
		Query: &users.SearchUsersQuery{
			Operator: `AND`,
			Operands: []map[string]any{{"filter_name": "email_address", "filter_value": []string{email}}},
		},
	})
	if err != nil {
		log.Printf("something went wrong searching for user: %s\n", err)
	}

	if len(srchresp.Results) == 0 {
		log.Printf("found 0 users for email: %s\n", email)
		return false
	}
	log.Printf("found %v users for email %s", len(srchresp.Results), email)
	return true
}

// this is the endpoint the link in the magic link hits takes the token from the
// link's query params and hits the stytch authenticate endpoint to verify the token is valid
func (c *config) authenticate(w http.ResponseWriter, r *http.Request) {
	var token string
	if TESTING {
		token = "DOYoip3rvIMMW5lgItikFK-Ak1CfMsgjuiCyI7uuU94="
	} else {
		token = r.URL.Query().Get("token")
	}
	resp, err := c.stytchClient.MagicLinks.Authenticate(
		context.Background(),
		&magiclinks.AuthenticateParams{
			Token:                  token,
			SessionDurationMinutes: 60,
		})
	if err != nil {
		log.Printf("something went wrong authenticating the magic link: %s\n", err)
	}

	log.Println("printing response writer header before save....")
	for name, values := range w.Header() {
		// Loop over all values for the name.
		for _, value := range values {
			log.Println(name, value)
		}
	}

	session, err := store.Get(r, "stytch_session")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("storing session token: %v", resp.SessionToken)

	session.Values["token"] = resp.SessionToken
	session.Save(r, w)

	log.Println("printing response writer header after save....")
	for name, values := range w.Header() {
		// Loop over all values for the name.
		for _, value := range values {
			log.Println(name, value)
		}
	}

	c.homepage(w, r)
}

// handles the logout endpoint
func (c *config) logout(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "stytch_session")
	if err != nil {
		log.Printf("error getting gorilla session: %s\n", err)
	}
	session.Options.MaxAge = -1
	session.Save(r, w)

	parseAndExecuteTemplate("templates/loggedOut.html", nil, w)
}

// handles returning the authenticated user, if valid session present
func (c *config) getAuthenticatedUser(w http.ResponseWriter, r *http.Request) *users.User {
	session, err := store.Get(r, "stytch_session")
	if err != nil || session == nil {
		return nil
	}

	token, ok := session.Values["token"].(string)
	if !ok || token == "" {
		return nil
	}

	resp, err := c.stytchClient.Sessions.Authenticate(
		context.Background(),
		&sessions.AuthenticateParams{
			SessionToken: token,
		})
	if err != nil {
		delete(session.Values, "token")
		session.Save(r, w)
		return nil
	}
	session.Values["token"] = resp.SessionToken
	session.Save(r, w)

	return &resp.User
}

// handles the Men's socks page for Hello Socks
func (c *config) menscollection(w http.ResponseWriter, r *http.Request) {
	user := c.getAuthenticatedUser(w, r)
	if user != nil {
		c.product(
			w,
			"Men",
			[]Product{{Name: "Green", Imagename: "greensocks.png"}, {Name: "Blue", Imagename: "bluesocks.png"}},
		)
	} else {
		parseAndExecuteTemplate(
			"templates/forbidden.html",
			nil,
			w,
		)
	}
}

// handles the Women's socks page for Hello Socks
func (c *config) womenscollection(w http.ResponseWriter, r *http.Request) {
	user := c.getAuthenticatedUser(w, r)
	if user != nil {
		c.product(
			w,
			"Women",
			[]Product{{Name: "Pink", Imagename: "pinksocks.png"}, {Name: "Yellow", Imagename: "yellowflowersocks.png"}},
		)
	} else {
		parseAndExecuteTemplate(
			"templates/forbidden.html",
			nil,
			w,
		)
	}
}

// handles the Kid's socks page for Hello Socks
func (c *config) kidscollection(w http.ResponseWriter, r *http.Request) {
	user := c.getAuthenticatedUser(w, r)
	if user != nil {
		c.product(
			w,
			"Kid",
			[]Product{{Name: "Pink/Yellow", Imagename: "kidspinkyellowsocks.png"}, {Name: "Blue/Pink", Imagename: "kidsbluepinksocks.png"}},
		)
	} else {
		parseAndExecuteTemplate(
			"templates/forbidden.html",
			nil,
			w,
		)
	}
}

// handles each product category page for Hello Socks
func (c *config) product(w http.ResponseWriter, who string, data []Product) {
	//user := c.getAuthenticatedUser(w, r)

	//if user != nil {
	linksmap := make(map[string]template.HTML)
	linksmap["Women"] = template.HTML(`<a href="/women">Women</a>`)
	linksmap["Men"] = template.HTML(`<a href="/men">Men</a>`)
	linksmap["Kids"] = template.HTML(`<a href="/kids">Kids</a>`)
	parseAndExecuteTemplate(
		"templates/product.html",
		&templateVariables{
			Who:      who,
			Products: data,
			NavLinks: linksmap,
		},
		w,
	)
	//} else {
	//	parseAndExecuteTemplate(
	//		"templates/forbidden.html",
	//		nil,
	//		w,
	//	)
	//}

}

// helper function to parse the template & render it with any provided data
func parseAndExecuteTemplate(temp string, templateVars *templateVariables, w http.ResponseWriter) {
	t, err := template.ParseFiles(temp)
	if err != nil {
		log.Printf("something went wrong parsing template: %s\n", err)
	}

	err = t.Execute(w, templateVars)
	if err != nil {
		log.Printf("something went wrong executing the template: %s\n", err)
	}
}

// helper function so see if a key is in the .env file
// if so return that value, otherwise return the default value
func getEnv(key string, defaultValue string) string {
	/* value, exists := os.LookupEnv(key)
	if value, exists = os.LookupEnv(key); exists {
		return value
	}
	return defaultValue */
	//fixed the original 'dodgy' code above

	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue

}

// helper function to load in the .env file & set config values
func initializeConfig() (*config, error) {
	if err := godotenv.Load(".env.local"); err != nil {
		log.Printf("No .env file found at '%s'", ".env.local")
		return &config{}, errors.New("error loading .env.local file")
	}
	address := getEnv("ADDRESS", "localhost:3000")

	useWhitelist := getEnv("USEWHITELIST", "false")

	// define the stytch client using your stytch project id & secret
	// use stytch.EnvLive if you want to hit the live api
	stytchAPIClient, err := stytchapi.NewClient(
		os.Getenv("STYTCH_PROJECT_ID"),
		os.Getenv("STYTCH_SECRET"),
	)
	if err != nil {
		log.Fatalf("error instantiating API client %s", err)
	}

	return &config{
		address:      address,
		fullAddress:  "http://" + address,
		stytchClient: stytchAPIClient,
		whitelist:    useWhitelist == "true",
	}, nil

}
