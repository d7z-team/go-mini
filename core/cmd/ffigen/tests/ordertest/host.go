package ordertest

import (
	"errors"
	"fmt"
)

type Item struct {
	Name  string
	Price float64
}

type Order struct {
	ID     string
	Items  []Item
	Closed bool
}

type OrderImpl struct{}

func NewOrderImpl() *OrderImpl {
	return &OrderImpl{}
}

func (b *OrderImpl) New(id string) (*Order, error) {
	return &Order{ID: id}, nil
}

func (b *OrderImpl) AddItem(o *Order, name string, price float64) error {
	if o == nil {
		return errors.New("nil order")
	}
	if o.Closed {
		return fmt.Errorf("order %s is closed", o.ID)
	}
	o.Items = append(o.Items, Item{Name: name, Price: price})
	return nil
}

func (b *OrderImpl) GetTotal(o *Order) (float64, error) {
	if o == nil {
		return 0, errors.New("nil order")
	}
	total := 0.0
	for _, it := range o.Items {
		total += it.Price
	}
	return total, nil
}

func (b *OrderImpl) Close(o *Order) error {
	if o == nil {
		return errors.New("nil order")
	}
	o.Closed = true
	return nil
}
