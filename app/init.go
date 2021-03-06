package app

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/revel/revel"
	"github.com/xaionaro-go/mswfAPI/app/common"
	"github.com/xaionaro-go/networkControl"
	linuxHost "github.com/xaionaro-go/networkControl/hosts/linux"
	"log"
	// FWSMHost "github.com/xaionaro-go/networkControl/hosts/fwsm"
	"os/exec"
	"runtime"
	"strings"
)

const (
	SMART_LOGGER_TRACEBACK_DEPTH int = 10
)

var (
	// AppVersion revel app version (ldflags)
	AppVersion string

	// BuildTime revel app build-time (ldflags)
	BuildTime string

	NetworkHosts networkControl.Hosts
)

func CheckLoginPass(login, pass string) bool {
	sha1HashBytes := sha1.Sum([]byte(pass))
	sha1Hash := hex.EncodeToString(sha1HashBytes[:])
	for i := 0; true; i++ {
		cfgKey := fmt.Sprintf("user%v.login", i)
		loginCheck, ok := revel.Config.String(cfgKey)
		if !ok {
			revel.AppLog.Debug("there's no configuration option", cfgKey)
			break
		}
		if login != loginCheck {
			revel.AppLog.Debug("login check: ", login, "<>", loginCheck)
			continue
		}
		sha1HashCheck, ok := revel.Config.String(fmt.Sprintf("user%v.password_sha1", i))
		if !ok {
			var passCheck string
			passCheck, ok = revel.Config.String(fmt.Sprintf("user%v.password", i))
			sha1HashBytesCheck := sha1.Sum([]byte(passCheck))
			sha1HashCheck = hex.EncodeToString(sha1HashBytesCheck[:])
		}
		if !ok {
			revel.AppLog.Errorf("Shouldn't happened")
			continue
		}
		if sha1Hash != sha1HashCheck {
			revel.AppLog.Debugf("sha1 check failed: %v: %v %v %v %v", login, len(sha1Hash), len(sha1HashCheck), sha1Hash, sha1HashCheck)
			continue
		}

		revel.AppLog.Infof("Authed as %v", login)
		return true
	}

	return false
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func ReadConfig() {
	exec.Command("git", "-C", common.FWSM_CONFIG_PATH, "stash").Run()
	checkErr(common.ReadConfig())
}

type smartLogger struct {
	printf func(fmt string, args ...interface{})
}

func (l smartLogger) Write(p []byte) (n int, err error) {
	s := string(p)

	where := ""

	for i := 2; i < 32; i++ {
		_, filePath, _, ok := runtime.Caller(i)
		if !ok {
			break
		}
		if filePath == `<autogenerated>` {
			continue
		}
		if strings.HasPrefix(filePath, "/usr/lib/go") {
			continue
		}

		whereArray := make([]string, SMART_LOGGER_TRACEBACK_DEPTH, SMART_LOGGER_TRACEBACK_DEPTH)
		for j := 0; j < SMART_LOGGER_TRACEBACK_DEPTH; j++ {
			_, filePath, line, ok := runtime.Caller(i + j)
			pathParts := strings.Split(filePath, "/")
			fileName := pathParts[len(pathParts)-1]
			if !ok || strings.HasSuffix(fileName, ".s") {
				whereArray = whereArray[SMART_LOGGER_TRACEBACK_DEPTH-j:]
				break
			}
			whereArray[SMART_LOGGER_TRACEBACK_DEPTH-1-j] = fmt.Sprintf("%v:%v", fileName, line)
		}
		where = "[" + strings.Join(whereArray, " -> ") + "] "
		break
	}

	l.printf("%v %s", where, s)

	return len(p), nil
}

func InitNetworkControl() {
	NetworkHosts = append(NetworkHosts, linuxHost.NewHost(nil))
	/*NetworkHosts = append(NetworkHosts, FWSMHost.NewHost(&FWSMHost.AccessDetails{
		Host: "10.0.0.99",
		Slot: 4,
		Processor: 1,
		EntryPassword: fwsmEntryPassword,
		FWSMPassword:  fwsmFWSMPassword,
	}))*/

	NetworkHosts.SetLoggerDebug  (log.New(&smartLogger{printf: revel.AppLog.Debugf}, "", 0))
	NetworkHosts.SetLoggerInfo   (log.New(&smartLogger{printf: revel.AppLog.Infof}, "", 0))
	NetworkHosts.SetLoggerWarning(log.New(&smartLogger{printf: revel.AppLog.Warnf}, "", 0))
	NetworkHosts.SetLoggerError  (log.New(&smartLogger{printf: revel.AppLog.Errorf}, "", 0))
	NetworkHosts.SetLoggerPanic  (log.New(&smartLogger{printf: revel.AppLog.Panicf}, "", 0))

	RestoreNetworkFromDisk()
	//NetworkHosts.RescanState() // it's already rescanned while restoring from the disk
}

func RestoreNetworkFromDisk() {
	err := NetworkHosts.RestoreFromDisk()
	if err != nil {
		panic(fmt.Sprintf("Got an error: %v", err.Error()))
	}
}

func init() {
	// Filters is the default set of global filters.
	revel.Filters = []revel.Filter{
		revel.PanicFilter,             // Recover from panics and display an error page instead.
		revel.RouterFilter,            // Use the routing table to select the right Action
		revel.FilterConfiguringFilter, // A hook for adding or removing per-Action filters.
		revel.ParamsFilter,            // Parse parameters into Controller.Params.
		revel.SessionFilter,           // Restore and write the session cookie.
		revel.FlashFilter,             // Restore and write the flash cookie.
		revel.ValidationFilter,        // Restore kept validation errors and save new ones from cookie.
		revel.I18nFilter,              // Resolve the requested language
		HeaderFilter,                  // Add some security based headers
		revel.InterceptorFilter,       // Run interceptors around the action.
		revel.CompressFilter,          // Compress the result.
		ActionInvoker,                 // Invoke the action.
	}

	// Register startup functions with OnAppStart
	// revel.DevMode and revel.RunMode only work inside of OnAppStart. See Example Startup Script
	// ( order dependent )
	// revel.OnAppStart(ExampleStartupScript)
	// revel.OnAppStart(InitDB)
	revel.OnAppStart(ReadConfig)
	revel.OnAppStart(InitNetworkControl)
}

// HeaderFilter adds common security headers
// There is a full implementation of a CSRF filter in
// https://github.com/revel/modules/tree/master/csrf
var HeaderFilter = func(c *revel.Controller, fc []revel.Filter) {
	c.Response.Out.Header().Add("X-Frame-Options", "SAMEORIGIN")
	c.Response.Out.Header().Add("X-XSS-Protection", "1; mode=block")
	c.Response.Out.Header().Add("X-Content-Type-Options", "nosniff")
	c.Response.Out.Header().Add("Referrer-Policy", "strict-origin-when-cross-origin")

	fc[0](c, fc[1:]) // Execute the next filter stage.
}

func claimsUserToUserInfo(claimsUser map[string]interface{}) common.UserInfo {
	return common.UserInfoFromClaimsUser(claimsUser)
}

func tryParseJWT(c *revel.Controller) {
	var tokenString string
	c.Params.Bind(&tokenString, "token")
	if tokenString == "" {
		authString := c.Request.GetHttpHeader("Authorization")
		if !strings.HasPrefix(tokenString, "Basic ") {
			words := strings.Split(authString, " ")
			if len(words) >= 2 {
				loginPassEncoded := words[1]
				loginPass, err := base64.StdEncoding.DecodeString(loginPassEncoded)
				if err != nil {
					revel.AppLog.Errorf("Cannot decode base64 string: \"%v\": %v", loginPassEncoded, err.Error())
					return
				}
				loginPassWords := strings.Split(string(loginPass), ":") // TODO: consider character ":" in the password

				if len(loginPassWords) >= 2 {
					if CheckLoginPass(loginPassWords[0], loginPassWords[1]) {
						c.ViewArgs["me"] = common.UserInfo{loginPassWords[0], true, true}
					}
				}
			}
			return
		}

		tokenString := authString
		revel.AppLog.Debugf("tokenString from the header == <%v>", tokenString)
		if !strings.HasPrefix(tokenString, "Bearer ") {
			return // No authorization information is passed
		}
		tokenString = strings.Split(tokenString, " ")[1]

	}
	if tokenString == "" {
		revel.AppLog.Errorf("tokenString is empty")
		return // this shouldn't happened, see above
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
		}

		jwtSecret, ok := revel.Config.String("jwt_secret")
		if !ok {
			revel.AppLog.Errorf("Shouldn't happened")
			panic("Internal error")
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		revel.AppLog.Errorf("Got error: %v; token:<%v>", err.Error(), tokenString)
		return
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		c.ViewArgs["me"] = claimsUserToUserInfo(claims["user"].(map[string]interface{}))
	}

	return
}

var ActionInvoker = func(c *revel.Controller, f []revel.Filter) {
	c.ViewArgs["me"] = common.UserInfo{}
	tryParseJWT(c)

	revel.ActionInvoker(c, f)
}

//func ExampleStartupScript() {
//	// revel.DevMod and revel.RunMode work here
//	// Use this script to check for dev mode and set dev/prod startup scripts here!
//	if revel.DevMode == true {
//		// Dev mode
//	}
//}
