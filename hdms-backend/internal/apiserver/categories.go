package apiserver

import (
	"net/http"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) ListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := s.catalog.ListCategories(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.Category, 0, len(categories))
	for _, c := range categories {
		items = append(items, categoryToGen(c))
	}
	writeJSON(w, http.StatusOK, gen.CategoryList{Items: items})
}

func (s *Server) CreateCategory(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.CreateCategoryRequest](w, r)
	if !ok {
		return
	}
	category, err := s.catalog.CreateCategory(r.Context(), catalogapi.CreateCategoryParams{
		Name:              req.Name,
		DefaultLoanPeriod: secondsPtrToDuration(req.DefaultLoanPeriodSeconds),
		RequiresApproval:  boolFromPtr(req.RequiresApproval),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, categoryToGen(category))
}

func (s *Server) UpdateCategory(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.UpdateCategoryRequest](w, r)
	if !ok {
		return
	}
	category, err := s.catalog.UpdateCategory(r.Context(), id, catalogapi.UpdateCategoryParams{
		Name:              req.Name,
		DefaultLoanPeriod: secondsPtrToDuration(req.DefaultLoanPeriodSeconds),
		RequiresApproval:  boolFromPtr(req.RequiresApproval),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, categoryToGen(category))
}

func categoryToGen(c catalogapi.Category) gen.Category {
	var seconds *int64
	if c.DefaultLoanPeriod != nil {
		s := int64(*c.DefaultLoanPeriod / time.Second)
		seconds = &s
	}
	return gen.Category{
		Id:                       c.ID,
		Name:                     c.Name,
		DefaultLoanPeriodSeconds: seconds,
		RequiresApproval:         c.RequiresApproval,
		CreatedAt:                c.CreatedAt,
	}
}

func secondsPtrToDuration(seconds *int64) *time.Duration {
	if seconds == nil {
		return nil
	}
	d := time.Duration(*seconds) * time.Second
	return &d
}

func boolFromPtr(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
