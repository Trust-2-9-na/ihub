# DSI Golang Web Service Framework

## Project Structure

To start a project, clone this repository and create a `.env` file in the root, and populate it with the following data:

```.env
DB_DATABASE=forms
DB_USERNAME=user
DB_PASSWORD=password
DB_PORT=5432
DB_HOST=127.0.0.1
PORT=9000
```

The port is use for serving http traffic, and the database connection is used for the database connection.

***NOTE:*** This project should be used winth a PostgreSQL database

The above config shows that the web service will run on `http;//localhost:9000`

```bash
├── cmd
│   └── main.go
├── go.mod
├── go.sum
├── libs
│   ├── configs
│   │   └── config.go
│   ├── database
│   │   ├── base.go
│   │   ├── database.go
│   │   └── migrations
│   │       └── migrate.go
│   ├── handlers
│   │   └── system_errors.go
│   └── middlewares
│       └── api.go
├── README.md
└── services
    ├── api
    │   ├── controllers
    │   │   ├── constructor.go
    │   │   └── hello_world.go
    │   ├── jobs
    │   │   └── jobs.go
    │   ├── middlewares
    │   │   └── middleware.go
    │   ├── models
    │   │   └── models.go
    │   └── routes.go
    ├── router.go
    └──
``` server.go

## Executable Binary

To create an executable binary, run the following command:

```bash
go build -o 25DService cmd/main.go
```

The above command creates a framework that cam be invoked by executing,

```bash
./25dService
```

***NOTE:**** While compiling the binary requires golang installed, execution does not.

To run in development mode, run the following command:

```bash
go run cmd/main.go
```

## Development

In development, one needs to run: `go mod tidy`.

The framework is structured in a way that each respective service can be self containing. By default, the project comes with the `api` service.

The service name can be renamed to any name, but the name of the service should be reflected in the top level `router.go` file import path.

If for example, you want to change the service name to `hello`, you need to change the import path in the `router.go` file to `hello`, change the package name in the package specific file `routes.go` to `hello`.

These changes should be done as shown below:

Change: 

`router.go`

```diff
package services

import (
	"web/libs/middlewares"
-   "web/services/api"
+   "web/services/hello"
)

func (s *Server) RegisterRoutes() *mux.Router {
	router := mux.NewRouter()
	router.Use(middlewares.DefaultAPIHeader())
-   api.NewRouter(router, s.db.DB())
+   hello.NewRouter(router, s.db.DB())
    return router
}
```

And

`hello/routes.go`
```diff
- package api
+ package hello

import (
-   "web/services/api/controllers"
+   "web/services/hello/controllers"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

func NewRouter(r *mux.Router, DB *gorm.DB) {
    	c := &controllers.Construct{
		DB: DB,
	}
	route := r.PathPrefix("/api").Subrouter()
	route.HandleFunc("", c.HellowWorld).Methods("GET")
}
```
This change is also required if copying the default `api` service as a separate service.

## Migrations

The frameowkr sypports migrations, utilising the [gorm](https://gorm.io/docs/migration.html) package.

    NOTE: AutoMigrate will create tables, missing foreign keys, constraints, columns and indexes. It will change existing column’s type if its size, precision changed, or if it’s changing from non-nullable to nullable. It WON’T delete unused columns to protect your data.

### Creating a migration

The framework requires that the model to be migrated is in the `models` folder of a specific service folder. e.g: `services/api/models`. The actual migration to be run should be specified in the file `web/libs/database/migratios/migrate.go`.

The example below shows how to migrate a model called `Log` from the `api` service.

```go
package migrations

import (
	"web/libs/database"
	"web/services/api/models"
)

func Migrate() error {
	db := database.New()
	return db.DB().AutoMigrate(
		&models.Log{},
	)
}
```
The above migration will create a table called `logs` in the database.

If you have more models to migrate, you can add them to the `AutoMigrate` function, as shown below.

```diff
func Migrate() error {
	db := database.New()
	return db.DB().AutoMigrate(
         &models.Log{},
+        &models.User{},
+        &models.Form{},
+        &models.FormField{},
    )
}
```

This will result in:

```go
func Migrate() error {
	db := database.New()
	return db.DB().AutoMigrate(
		&models.Log{},
        &models.User{},
        &models.Form{},
        &models.FormField{},
	)
}
```

To run this migration, run the following command:

```bash
go run cmd/main.go -migrate
```

or, if using compiled binary

```bash
./25dService -migrate
```
The above command assumes you compiled a binary with the name `25dService`.

## Database CRUD

The perform crud operations, the framework utilises GORM. Please refer to the gorm documentation for this, at [https://gorm.io/docs/index.html](https://gorm.io/docs/index.html).

### Example Create Operation

To create for example a log file, you can use the following code in your service controller:

```go
func (c *Construct) HellowWorld(w http.ResponseWriter, r *http.Request) {
    if err := c.DB.Create(&models.Log{
		Activity: "Hello World",
	}).Error; err != nil {
		c.Json(w, http.Status, "error", map[string]interface{}{
		    "message": "unable to create log",
	    })
		return
	}
	c.Json(w, http.StatusOK, "ok", map[string]interface{}{
		"message": "logs created",
	})
}
```
