package valueobject

// Address represents a physical address
type Address struct {
    Street   string
    City     string
    Province string
    PostalCode string
    Country  string
}

// NewAddress creates a new Address
func NewAddress(street, city, province, postalCode, country string) Address {
    return Address{
        Street:     street,
        City:       city,
        Province:   province,
        PostalCode: postalCode,
        Country:    country,
    }
}

// String returns a formatted address string
func (a Address) String() string {
    return a.Street + ", " + a.City + ", " + a.Province + " " + a.PostalCode + ", " + a.Country
}
