package db

var RegionNames = []string{"North America", "South America", "Europe", "Oceania", "Asia"}

var ProductAdjectives = []string{"Vintage", "Modern", "Eco friendly", "Luxury", "High end", "Enthusiast", "Refurbished"}
var ProductCategories = []string{"Appliances", "Clothing", "Books", "Miscellaneous", "Kitchenware", "Computers", "Furniture", "Beauty"}
var ProductNames = make([]string, 0, len(ProductAdjectives)*len(ProductCategories))

var CustomerFirstNames = []string{
	"Javier",
	"Westbrooke",
	"Norry",
	"Cody",
	"Marlow",
	"Dillie",
	"Arie",
	"Vernen",
	"Phil",
	"Emmerich",
}
var CustomerLastNames = []string{
	"Lupins",
	"Broad",
	"Petroula",
	"Tofts",
	"Montrose",
	"Simmons",
	"Teaser",
	"Clemenceau",
	"Lamb",
	"Kimble",
	"Essam",
	"Hurleigh",
	"Bonifacio",
	"Huller",
	"Grut",
}

var StoreAdjectives = []string{"Super", "Mega", "Mix", "Fast"}
var StoreBrands = []string{"Mart", "Store", "Shop"}
var StoreNames = make([]string, 0, len(StoreAdjectives)*len(StoreBrands))
