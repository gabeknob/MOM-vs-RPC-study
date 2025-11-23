package main

import (
	"simulation/pkg/db"
)

func main() {
	database := db.Init("./database.db")

	migrator := database.Migrator()
	dropTableErr := migrator.DropTable(&db.Sale{}, &db.Product{}, &db.Customer{}, &db.Store{}, &db.Region{})
	if dropTableErr != nil {
		return
	}

	migrationErr := database.AutoMigrate(&db.Sale{}, &db.Product{}, &db.Customer{}, &db.Store{}, &db.Region{})
	if migrationErr != nil {
		return
	}

	var regions = make([]db.Region, len(db.RegionNames))
    var products = make([]db.Product, len(db.ProductAdjectives)*len(db.ProductCategories))
    var customers = make([]db.Customer, 0, len(db.CustomerFirstNames)*len(db.CustomerLastNames))
    var stores = make([]db.Store, len(db.StoreAdjectives)*len(db.StoreBrands))
    var sales = make([]db.Sale, 10000)
	populateRegions(&regions)
	result := database.Create(&regions)
	if result.Error != nil {
		panic(result.Error)
	}

	populateProducts(&products)
	result = database.Create(&products)
	if result.Error != nil {
		panic(result.Error)
	}

	populateCustomers(&customers)
	result = database.Create(&customers)
	if result.Error != nil {
		panic(result.Error)
	}

	populateStores(&stores, &regions)
	result = database.Create(&stores)
	if result.Error != nil {
		panic(result.Error)
	}

	populateSales(&sales, &stores, &products, &customers)
	result = database.CreateInBatches(&sales, 100)
	if result.Error != nil {
		panic(result.Error)
	}
}
