package httpx

import (
	"net/http"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/repository"
	"marketplace/internal/service"
)

const defaultPageSize = 20

func (s *Server) ListProducts(w http.ResponseWriter, r *http.Request, params api.ListProductsParams) {
	if _, ok := actor(w, r, service.RoleUser, service.RoleSeller, service.RoleAdmin); !ok {
		return
	}
	filter := repository.ProductFilter{Page: 0, Size: defaultPageSize}
	if params.Page != nil {
		filter.Page = *params.Page
	}
	if params.Size != nil {
		filter.Size = *params.Size
	}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	if params.Category != nil {
		filter.Category = *params.Category
	}
	products, total, err := s.Products.List(r.Context(), filter)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	page := api.ProductPage{Items: make([]api.ProductResponse, 0, len(products)), TotalElements: int64(total), Page: filter.Page, Size: filter.Size}
	for _, p := range products {
		page.Items = append(page.Items, toProductResponse(p))
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) GetProduct(w http.ResponseWriter, r *http.Request, id api.ProductId) {
	if _, ok := actor(w, r, service.RoleUser, service.RoleSeller, service.RoleAdmin); !ok {
		return
	}
	p, err := s.Products.Get(r.Context(), id)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProductResponse(p))
}

func (s *Server) CreateProduct(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r, service.RoleSeller, service.RoleAdmin)
	if !ok {
		return
	}
	var req api.ProductCreate
	if !decode(w, r, &req) {
		return
	}
	p, err := s.Products.Create(r.Context(), a, repository.Product{
		Name: req.Name, Description: req.Description, Price: req.Price, Stock: req.Stock,
		Category: req.Category, Status: string(req.Status),
	}, req.SellerId)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProductResponse(p))
}

func (s *Server) UpdateProduct(w http.ResponseWriter, r *http.Request, id api.ProductId) {
	a, ok := actor(w, r, service.RoleSeller, service.RoleAdmin)
	if !ok {
		return
	}
	var req api.ProductUpdate
	if !decode(w, r, &req) {
		return
	}
	p, err := s.Products.Update(r.Context(), a, repository.Product{
		ID: id, Name: req.Name, Description: req.Description, Price: req.Price, Stock: req.Stock,
		Category: req.Category, Status: string(req.Status),
	})
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProductResponse(p))
}

func (s *Server) DeleteProduct(w http.ResponseWriter, r *http.Request, id api.ProductId) {
	a, ok := actor(w, r, service.RoleSeller, service.RoleAdmin)
	if !ok {
		return
	}
	if err := s.Products.Archive(r.Context(), a, id); err != nil {
		apperr.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toProductResponse(p repository.Product) api.ProductResponse {
	return api.ProductResponse{
		Id: p.ID, Name: p.Name, Description: p.Description, Price: p.Price, Stock: p.Stock,
		Category: p.Category, Status: api.ProductStatus(p.Status), SellerId: p.SellerID,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}
