package mom

type SalesRequest struct {
	Region    string `json:"region"`
	Category  string `json:"category"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`

	// Pointers to detect null/nil
	StartHour *int32 `json:"start_hour"`
	EndHour   *int32 `json:"end_hour"`

	BuyerKeyword   string `json:"buyer_keyword"`
	ProductKeyword string `json:"product_keyword"`
}

type SalesResponse struct {
	TotalSalesCount int64  `json:"total_sales_count"`
	TotalRevenue    int64  `json:"total_revenue"`
	RegionProcessed string `json:"region_processed"`
	Error           string `json:"error,omitempty"` // omitempty: don't send this field if it's empty
}
