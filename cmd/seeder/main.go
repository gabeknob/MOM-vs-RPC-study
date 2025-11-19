package main

import "simulation/pkg/db"

func main() {
	database := db.Init("./database.db")

	migrator := database.Migrator()
	err := migrator.DropTable(&db.Sale{}, &db.Product{}, &db.Store{}, &db.Region{})
	if err != nil {
		return
	}

	err = database.AutoMigrate(&db.Sale{}, &db.Product{}, &db.Store{}, &db.Region{})
	if err != nil {
		return
	}

	regions := []db.Region{{Name: "North America"}, {Name: "South America"}, {Name: "Europe"}, {Name: "Oceania"}, {Name: "Asia"}}

	result := database.Create(&regions)

	if result.Error != nil {
		panic(result.Error)
	}
}
