package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// Структура для лога запросов
type RequestLog struct {
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Query      string    `json:"query,omitempty"`
	RemoteAddr string    `json:"remote_addr"`
	Timestamp  time.Time `json:"timestamp"`
}

type Product struct {
	ProductID      string            `json:"product_id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Price          Price             `json:"price"`
	Category       string            `json:"category"`
	Brand          string            `json:"brand"`
	Stock          Stock             `json:"stock"`
	SKU            string            `json:"sku"`
	Tags           []string          `json:"tags"`
	Images         []Image           `json:"images"`
	Specifications map[string]string `json:"specifications"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
	Index          string            `json:"index"`
	StoreID        string            `json:"store_id"`
}

type Price struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type Stock struct {
	Available int `json:"available"`
	Reserved  int `json:"reserved"`
}

type Image struct {
	URL string `json:"url"`
	Alt string `json:"alt"`
}

type RequestLogCodec struct{}

func (uc *RequestLogCodec) Encode(value any) ([]byte, error) {
	if _, isRequestLog := value.(*RequestLog); !isRequestLog {
		return nil, fmt.Errorf("expected type is *RequestLog, received %T", value)
	}
	return json.Marshal(value)
}

func (uc *RequestLogCodec) Decode(data []byte) (any, error) {
	var (
		c   RequestLog
		err error
	)
	err = json.Unmarshal(data, &c)
	if err != nil {
		return nil, fmt.Errorf("deserialization error: %v", err)
	}
	return &c, nil
}
