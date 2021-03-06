package main

import (
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// handler for http://andesite/login
func handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	_, ok := session.Values["user"]
	if ok {
		w.Header().Add("Location", "./files/")
	} else {
		urlR, _ := url.Parse(oauth2Provider.oa2baseURL + oauth2Provider.authorizeURL)
		parameters := url.Values{}
		parameters.Add("client_id", oauth2AppID)
		parameters.Add("redirect_uri", fullHost(r)+httpBase+"callback")
		parameters.Add("response_type", "code")
		parameters.Add("scope", oauth2Provider.scope)
		parameters.Add("duration", "temporary")
		parameters.Add("state", "none")
		urlR.RawQuery = parameters.Encode()
		w.Header().Add("Location", urlR.String())
	}
	w.Header().Add("cache-control", "no-store")
	w.WriteHeader(301)
}

// handler for http://andesite/callback
func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if len(code) == 0 {
		return
	}
	parameters := url.Values{}
	parameters.Add("client_id", oauth2AppID)
	parameters.Add("client_secret", oauth2AppSecret)
	parameters.Add("grant_type", "authorization_code")
	parameters.Add("code", code)
	parameters.Add("redirect_uri", fullHost(r)+httpBase+"callback")
	parameters.Add("state", "none")
	urlR, _ := url.Parse(oauth2Provider.oa2baseURL + oauth2Provider.tokenURL)
	req, _ := http.NewRequest("POST", urlR.String(), strings.NewReader(parameters.Encode()))
	req.Header.Set("User-Agent", "nektro/andesite")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+b64.StdEncoding.EncodeToString([]byte(oauth2AppID+":"+oauth2AppSecret)))
	req.Header.Set("Accept", "application/json")
	client := &http.Client{}
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	var respJSON OAuth2CallBackResponse
	json.Unmarshal(body, &respJSON)
	session := getSession(r)
	session.Values[accessToken] = respJSON.AccessToken
	session.Save(r, w)
	w.Header().Add("Location", "./token")
	w.Header().Add("cache-control", "no-store")
	w.WriteHeader(301)
}

// handler for http://andesite/token
func handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	val, ok := session.Values[accessToken]
	if !ok {
		return
	}
	urlR, _ := url.Parse(oauth2Provider.oa2baseURL + oauth2Provider.meURL)
	req, _ := http.NewRequest("GET", urlR.String(), strings.NewReader(""))
	req.Header.Set("User-Agent", "nektro/andesite")
	req.Header.Set("Authorization", "Bearer "+val.(string))
	client := &http.Client{}
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	var respMe map[string]interface{}
	json.Unmarshal(body, &respMe)
	session.Values["user"] = fixID(respMe["id"])
	session.Values["name"] = respMe[oauth2Provider.nameProp].(string)
	session.Save(r, w)
	w.Header().Add("Location", "./files/")
	w.Header().Add("cache-control", "no-store")
	w.WriteHeader(301)
}

// handler for http://andesite/test
func handleTest(w http.ResponseWriter, r *http.Request) {
	// sessions test
	// increment number every refresh
	session := getSession(r)
	i, ok := session.Values["int"]
	if !ok {
		i = 0
	}
	j := i.(int)
	session.Values["int"] = j + 1
	session.Save(r, w)
	w.Header().Add("cache-control", "no-store")
	fmt.Fprintf(w, strconv.Itoa(j))
}

// handler for http://andesite/files/*
func handleFileListing(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.RequestURI, "..") {
		return
	}
	session := getSession(r)
	sessID, ok := session.Values["user"]
	if !ok {
		writeUserDenied(w, r, true, true)
		return
	}
	userID := sessID.(string)

	// get path
	qpath, _ := url.QueryUnescape(r.RequestURI[6:])

	// valid path check
	stat, err := rootDir.Stat(qpath)
	if os.IsNotExist(err) {
		// 404
		writeUserDenied(w, r, true, false)
		return
	}

	// server file/folder
	if stat.IsDir() {
		w.Header().Add("Content-Type", "text/html")

		// get list of all files
		files, _ := rootDir.ReadDir(qpath)

		// hide dot files
		files = filter(files, func(x os.FileInfo) bool {
			return !strings.HasPrefix(x.Name(), ".")
		})

		// amount of files in the directory
		l1 := len(files)

		// access check
		acc := queryAccess(userID)
		files = filter(files, func(x os.FileInfo) bool {
			ok := false
			fpath := qpath + x.Name()
			for _, item := range acc {
				if strings.HasPrefix(item, fpath) || strings.HasPrefix(qpath, item) {
					ok = true
				}
			}
			return ok
		})

		// amount of files given access to
		l2 := len(files)

		if l1 > 0 && l2 == 0 {
			writeUserDenied(w, r, true, false)
			return
		}

		data := make([]map[string]string, len(files))
		gi := 0
		for i := 0; i < len(files); i++ {
			name := files[i].Name()
			a := ""
			if files[i].IsDir() || files[i].Mode()&os.ModeSymlink != 0 {
				a = name + "/"
			} else {
				a = name
			}
			b := byteCountIEC(files[i].Size())
			c := files[i].ModTime().UTC().String()[:19]
			data[gi] = map[string]string{
				"name": a,
				"size": b,
				"mod":  c,
			}
			gi++
		}

		useruser, ok := queryUserBySnowflake(userID)
		admin := false
		if ok {
			admin = useruser.admin
		}

		sessName, _ := session.Values["name"]
		writeHandlebarsFile(w, "/listing.hbs", map[string]interface{}{
			"user":  userID,
			"path":  qpath,
			"files": data,
			"admin": admin,
			"base":  httpBase,
			"name":  oauth2Provider.namePrefix + sessName.(string),
		})
	} else {
		// access check
		can := false
		for _, item := range queryAccess(userID) {
			if strings.HasPrefix(qpath, item) {
				can = true
			}
		}
		if can == false {
			writeUserDenied(w, r, true, false)
			return
		}

		reader, _ := rootDir.ReadFile(qpath)
		io.Copy(w, reader)
	}
}

// handler for http://andesite/admin
func handleAdmin(w http.ResponseWriter, r *http.Request) {
	// get discord snowflake from session cookie
	session := getSession(r)
	sessID, ok := session.Values["user"]
	if !ok {
		writeUserDenied(w, r, false, true)
		return
	}
	userID := sessID.(string)

	// only allow admins
	useruser, ok := queryUserBySnowflake(userID)
	admin := false
	if ok {
		admin = useruser.admin
	}
	if !admin {
		writeUserDenied(w, r, false, false)
		return
	}

	//
	accesses := queryAllAccess()
	sessName, _ := session.Values["name"]
	writeHandlebarsFile(w, "/admin.hbs", map[string]interface{}{
		"user":     useruser.snowflake,
		"accesses": accesses,
		"base":     httpBase,
		"name":     oauth2Provider.namePrefix + sessName.(string),
	})
}

// handler for http://andesite/api/access/delete
func handleAccessDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIResponse(w, false, "This action requires using HTTP POST")
		return
	}
	//
	session := getSession(r)
	sessID, ok := session.Values["user"]
	if !ok {
		writeAPIResponse(w, false, "This action requires being logged in")
		return
	}
	userID := sessID.(string)
	//
	user, ok := queryUserBySnowflake(userID)
	if !ok {
		writeAPIResponse(w, false, "This action requires being a member of this server")
		return
	}
	if !user.admin {
		writeAPIResponse(w, false, "This action requires being a site administrator")
		return
	}
	//
	err := r.ParseForm()
	if err != nil {
		writeAPIResponse(w, false, "Error parsing form data")
		return
	}
	//
	aid := r.PostForm.Get("id")
	iid, err := strconv.ParseInt(aid, 10, 32)
	if err != nil {
		writeAPIResponse(w, false, "ID parameter must be an integer")
		return
	}
	//
	query(fmt.Sprintf("delete from access where id = '%d'", iid), true)
	writeAPIResponse(w, true, fmt.Sprintf("Removed access from %s.", r.PostForm.Get("snowflake")))
}

// handler for http://andesite/api/access/update
func handleAccessUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIResponse(w, false, "This action requires using HTTP POST")
		return
	}
	//
	session := getSession(r)
	sessID, ok := session.Values["user"]
	if !ok {
		writeAPIResponse(w, false, "This action requires being logged in")
		return
	}
	userID := sessID.(string)
	//
	user, ok := queryUserBySnowflake(userID)
	if !ok {
		writeAPIResponse(w, false, "This action requires being a member of this server")
		return
	}
	if !user.admin {
		writeAPIResponse(w, false, "This action requires being a site administrator")
		return
	}
	//
	err := r.ParseForm()
	if err != nil {
		writeAPIResponse(w, false, "Error parsing form data")
		return
	}
	//
	aid := r.PostForm.Get("id")
	iid, err := strconv.ParseInt(aid, 10, 32)
	if err != nil {
		writeAPIResponse(w, false, "ID parameter must be an integer")
		return
	}
	//
	queryPrepared("update access set path = ? where id = ?", true, r.PostForm.Get("path"), iid)
	writeAPIResponse(w, true, fmt.Sprintf("Updated access for %s.", r.PostForm.Get("snowflake")))
}

// handler for http://andesite/api/access/create
func handleAccessCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIResponse(w, false, "This action requires using HTTP POST")
		return
	}
	//
	session := getSession(r)
	sessID, ok := session.Values["user"]
	if !ok {
		writeAPIResponse(w, false, "This action requires being logged in")
		return
	}
	userID := sessID.(string)
	//
	user, ok := queryUserBySnowflake(userID)
	if !ok {
		writeAPIResponse(w, false, "This action requires being a member of this server")
		return
	}
	if !user.admin {
		writeAPIResponse(w, false, "This action requires being a site administrator")
		return
	}
	//
	err := r.ParseForm()
	if err != nil {
		writeAPIResponse(w, false, "Error parsing form data")
		return
	}
	//
	aid := queryLastID("access") + 1
	asn := r.PostForm.Get("snowflake")
	apt := r.PostForm.Get("path")
	//
	u, ok := queryUserBySnowflake(asn)
	aud := -1
	if ok {
		aud = u.id
	} else {
		aud = queryLastID("users") + 1
		queryPrepared("insert into users values (?, ?, ?)", true, aud, oauth2Provider.dbPrefix+asn, 0)
	}
	//
	queryPrepared("insert into access values (?, ?, ?)", true, aid, aud, apt)
	writeAPIResponse(w, true, fmt.Sprintf("Created access for %s.", asn))
}
