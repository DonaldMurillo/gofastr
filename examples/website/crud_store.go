package main

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/gofastr/gofastr/framework/ui"
)

// Customer is the entity for the CRUD demo. Kept intentionally tiny so
// the demo focuses on the architecture (SSR + island + form/validation +
// delete-confirm), not the entity shape.
type Customer struct {
	ID      int64
	Name    string
	Email   string
	Status  ui.StatusVariant // success / warning / danger / info / neutral
	Balance string           // pre-formatted; kept as string for the demo
}

// crudStore is an in-memory, mutex-guarded slice of Customers. Seeded
// from the existing demoCustomers list so the page has data on first
// load. Real apps swap this out for an entity-CRUD layer wired
// through framework.Entity.
type crudStore struct {
	mu      sync.RWMutex
	rows    []Customer
	nextID  int64
}

var customers = func() *crudStore {
	s := &crudStore{}
	for _, c := range demoCustomers {
		s.nextID++
		s.rows = append(s.rows, Customer{
			ID: s.nextID, Name: c.Name, Email: c.Email,
			Status: c.Status, Balance: c.Balance,
		})
	}
	return s
}()

func (s *crudStore) All() []Customer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Customer, len(s.rows))
	copy(out, s.rows)
	return out
}

func (s *crudStore) Get(id int64) (Customer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.rows {
		if c.ID == id {
			return c, nil
		}
	}
	return Customer{}, errors.New("not found")
}

func (s *crudStore) Add(c Customer) Customer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	c.ID = s.nextID
	if c.Status == "" {
		c.Status = ui.StatusNeutral
	}
	if c.Balance == "" {
		c.Balance = "$0.00"
	}
	s.rows = append(s.rows, c)
	return c
}

func (s *crudStore) Update(c Customer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.rows {
		if existing.ID == c.ID {
			s.rows[i] = c
			return nil
		}
	}
	return errors.New("not found")
}

func (s *crudStore) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.rows {
		if c.ID == id {
			s.rows = append(s.rows[:i], s.rows[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

// validateCustomer is the demo's validation pipeline. Matches the shape
// of framework.ValidationRegistry.Validate so a real app would just
// plug a registry in. Returns a FieldErrors map; empty = valid.
func validateCustomer(c Customer) ui.FieldErrors {
	errs := ui.FieldErrors{}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		errs["name"] = "Name is required."
	} else if len(name) < 2 {
		errs["name"] = "Name must be at least 2 characters."
	}
	email := strings.TrimSpace(c.Email)
	if email == "" {
		errs["email"] = "Email is required."
	} else if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		errs["email"] = "Please enter a valid email address."
	}
	if c.Status != "" {
		switch c.Status {
		case ui.StatusSuccess, ui.StatusWarning, ui.StatusDanger,
			ui.StatusInfo, ui.StatusNeutral:
		default:
			errs["status"] = "Pick a valid status."
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// parseCustomerForm extracts a Customer from form values. ID is parsed
// from a separate hidden "id" field for updates.
func parseCustomerForm(get func(string) string) Customer {
	c := Customer{
		Name:   strings.TrimSpace(get("name")),
		Email:  strings.TrimSpace(get("email")),
		Status: ui.StatusVariant(strings.TrimSpace(get("status"))),
	}
	if idStr := get("id"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			c.ID = id
		}
	}
	if b := strings.TrimSpace(get("balance")); b != "" {
		c.Balance = b
	}
	return c
}
