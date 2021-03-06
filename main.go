package main

import(
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4/middleware"
	"github.com/srinathgs/mysqlstore"
	"golang.org/x/crypto/bcrypt"
)

type Country struct {
	Code        string `json:"code,omitempty" db:"Code"`
	Name        string `json:"name,omitempty" db:"Name"`
	Continent   string `json:"continent,omitempty" db:"Continent"`
	Population  int    `json:"population,omitempty" db:"Population"`
}

type City struct {
	ID          int    `json:"id,omitempty"  db:"ID"`
	Name        string `json:"name,omitempty"  db:"Name"`
	CountryCode string `json:"countryCode,omitempty"  db:"CountryCode"`
	District    string `json:"district,omitempty"  db:"District"`
	Population  int    `json:"population,omitempty"  db:"Population"`
}

var(
	db *sqlx.DB
)

func main(){
	_db, err := sqlx.Connect("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8&parseTime=True&loc=Local", os.Getenv("DB_USERNAME"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOSTNAME"), os.Getenv("DB_PORT"), os.Getenv("DB_DATABASE")))
	if err != nil {
		log.Fatalf("Cannot Connect to Database: %s", err)
	}

	db=_db

	store,err := mysqlstore.NewMySQLStoreFromConnection(db.DB,"sessions","/",60*60*24*14,[]byte("secret-token"))
	if err != nil{
		panic(err)
	}

	e:=echo.New()
	e.Use(middleware.Logger())
	e.Use(session.Middleware(store))

	e.GET("/ping",func(c echo.Context)error{
		return c.String(http.StatusOK,"pong")
	})
	e.POST("/login",postLoginHandler)
	e.POST("/signup",postSignUpHandler)

	withLogin := e.Group("")
	withLogin.Use(checkLogin)

	withLogin.POST("/post",postCityInfoHandler)

	withLogin.GET("/countrylist",getCountryListHandler)

	withLogin.GET("/citylist/:countryCode",getCityListHandler)

	withLogin.GET("/cities/:cityName", getCityInfoHandler)
	withLogin.GET("/whoami",getWhoAmIHandler)
	e.Start(":4000")
}

type LoginRequestBody struct{
	Username string `json:"username,omitempty" form:"username"`
	Password string `json:"password,omitempty" form:"password"`
}

type User struct {
	Username string `json:"username,omitempty" db:"Username"`
	HashedPass string `json:"-" db:"HashedPass"`
}

func postSignUpHandler(c echo.Context)error{
	req:=LoginRequestBody{}
	c.Bind(&req)

	if req.Password == "" || req.Username == ""{
		return c.String(http.StatusBadRequest,"項目が空です")
	}

	hashedPass,err := bcrypt.GenerateFromPassword([]byte(req.Password),bcrypt.DefaultCost)
	if err != nil{
		return c.String(http.StatusInternalServerError, fmt.Sprintf("bcrypt generate error: %v",err))
	}

	var count int

	err = db.Get(&count,"SELECT COUNT(*) FROM users WHERE Username=?",req.Username)
	if err != nil{
		return c.String(http.StatusInternalServerError,fmt.Sprintf("db error %v",err))
	}

	if count > 0 {
		return c.String(http.StatusConflict,"ユーザーが既に存在しています")
	}

	_,err = db.Exec("INSERT INTO users (Username,HashedPass) VALUES (?,?)",req.Username,hashedPass)
	if err != nil{
		return c.String(http.StatusInternalServerError,fmt.Sprintf("db error %v",err))
	}
	return c.NoContent(http.StatusCreated)
}

func postLoginHandler(c echo.Context) error{
	req := LoginRequestBody{}
	c.Bind(&req)

	user := User{}
	err := db.Get(&user, "SELECT * FROM users WHERE username=?",req.Username)
	if err != nil{
		return c.String(http.StatusInternalServerError,fmt.Sprintf("db error %v",err))
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.HashedPass),[]byte(req.Password))
	if err != nil{
		if err == bcrypt.ErrMismatchedHashAndPassword{
			return c.NoContent(http.StatusForbidden)
		}else {
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	sess,err := session.Get("sessions",c)
	if err != nil{
		fmt.Println(err)
		return c.String(http.StatusInternalServerError,"something wrong in getting session")
	}
	sess.Values["userName"] = req.Username
	sess.Save(c.Request(),c.Response())
	return c.NoContent(http.StatusOK)
}

func checkLogin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error{
		sess,err := session.Get("sessions",c)
		if err != nil{
			fmt.Println(err)
			return c.String(http.StatusInternalServerError,"something wrong in getting session")
		}
		if sess.Values["userName"]==nil{
			return c.String(http.StatusForbidden,"please login")
		}
		c.Set("userName",sess.Values["userName"].(string))
		fmt.Println(sess.Values["userName"].(string))

		return next(c)
	}
}

type Me struct {
	Username string `json:"username,omitempty" db:"username"` 
}

func getWhoAmIHandler(c echo.Context)error{
	return c.JSON(http.StatusOK,Me{
		Username: c.Get("userName").(string),
	})
}

func getCountryListHandler(c echo.Context) error{
	countries := []Country{}
	err := db.Select(&countries,"SELECT Code,Name,Continent,Population FROM country")

	if err != nil{
		return c.String(http.StatusInternalServerError,fmt.Sprintf("db error %v",err))
	}

	return c.JSON(http.StatusOK,countries)
}

func getCityListHandler(c echo.Context) error{
	countryCode := c.Param("countryCode")
	cities :=[]City{}
	err := db.Select(&cities,"SELECT * FROM city WHERE CountryCode=?",countryCode)

	if err != nil{
		return c.String(http.StatusInternalServerError,fmt.Sprintf("db error %v",err))
	}

	return c.JSON(http.StatusOK,cities)
}

func getCityInfoHandler(c echo.Context) error{
	cityName := c.Param("cityName")
	fmt.Println(cityName)

	city := City{}
	db.Get(&city, "SELECT * FROM city WHERE Name=?", cityName)
	if city.Name == "" {
		return c.NoContent(http.StatusNotFound)
	}

	return c.JSON(http.StatusOK, city)
}

func postCityInfoHandler(c echo.Context)error{
	data := new(City)
	err := c.Bind(data)

	if err != nil{
		return c.String(http.StatusBadRequest,"failed to convert body to city_data")
	}

	db.Exec("INSERT INTO city (Name,CountryCode,District,Population) VALUES (?,?,?,?)",data.Name,data.CountryCode,data.District,data.Population)

	if err != nil{
		return c.String(http.StatusBadRequest,"failed to insert data")
	}

	return c.JSON(http.StatusOK,data)
}