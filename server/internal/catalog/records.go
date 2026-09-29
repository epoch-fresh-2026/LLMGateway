package catalog

// ChannelRecord is the persistence view of a channel, including the encrypted
// upstream key. Catalog owns encryption/decryption; stores only hold ciphertext.
type ChannelRecord struct {
	ID               int
	Name             string
	BaseURL          string
	APIKeyCiphertext string
	AuthType         string
	Status           int
	Weight           int
	Priority         int
	Balance          *string
}

// ChannelInsert is the persistence input for creating a channel.
type ChannelInsert struct {
	OwnerUserID      int
	Name             string
	BaseURL          string
	APIKeyCiphertext string
	AuthType         string
	Status           int
	Weight           int
	Priority         int
	Balance          *string
}

// ChannelUpdate is the persistence input for updating a channel. An empty
// APIKeyCiphertext keeps the stored ciphertext unchanged.
type ChannelUpdate struct {
	Name             string
	BaseURL          string
	AuthType         string
	Status           int
	Weight           int
	Priority         int
	Balance          *string
	APIKeyCiphertext string
}

// PricingRecord is the normalized persistence input for a pricing row. It is
// keyed by the upstream (real) model name, not the public alias.
type PricingRecord struct {
	ChannelID             int
	UpstreamModel         string
	InputPricePer1M       string
	OutputPricePer1M      string
	CachedInputPricePer1M string
	Currency              string
}
