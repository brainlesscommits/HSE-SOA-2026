package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"marketplace/internal/apperr"
	"marketplace/internal/repository"
)

type ProductService struct {
	DB   *repository.DB
	Repo repository.ProductRepository
}

func NewProductService(db *repository.DB) *ProductService {
	return &ProductService{DB: db, Repo: repository.ProductRepository{}}
}

func (s *ProductService) Get(ctx context.Context, id uuid.UUID) (repository.Product, error) {
	p, err := s.Repo.GetByID(ctx, s.DB.Pool, id)
	if repository.IsNotFound(err) {
		return p, productNotFound(id)
	}
	return p, err
}

func (s *ProductService) List(ctx context.Context, f repository.ProductFilter) ([]repository.Product, int, error) {
	return s.Repo.List(ctx, s.DB.Pool, f)
}

func (s *ProductService) Create(ctx context.Context, actor Actor, p repository.Product, sellerID *uuid.UUID) (repository.Product, error) {
	switch actor.Role {
	case RoleSeller:
		if sellerID != nil && *sellerID != actor.UserID {
			return repository.Product{}, apperr.AccessDenied()
		}
		p.SellerID = actor.UserID
	case RoleAdmin:
		p.SellerID = actor.UserID
		if sellerID != nil {
			p.SellerID = *sellerID
		}
	default:
		return repository.Product{}, apperr.AccessDenied()
	}
	p.ID = uuid.New()
	p.Price = roundMoney(p.Price)
	out, err := s.Repo.Create(ctx, s.DB.Pool, p)
	if repository.IsForeignKeyViolation(err) {
		return out, apperr.ValidationField("seller_id", "seller does not exist")
	}
	return out, err
}

func (s *ProductService) Update(ctx context.Context, actor Actor, p repository.Product) (repository.Product, error) {
	var out repository.Product
	err := s.DB.InTx(ctx, func(tx pgx.Tx) error {
		if err := s.checkOwner(ctx, tx, actor, p.ID); err != nil {
			return err
		}
		p.Price = roundMoney(p.Price)
		var err error
		out, err = s.Repo.Update(ctx, tx, p)
		return err
	})
	return out, err
}

func (s *ProductService) Archive(ctx context.Context, actor Actor, id uuid.UUID) error {
	return s.DB.InTx(ctx, func(tx pgx.Tx) error {
		if err := s.checkOwner(ctx, tx, actor, id); err != nil {
			return err
		}
		return s.Repo.Archive(ctx, tx, id)
	})
}

func (s *ProductService) checkOwner(ctx context.Context, q repository.Querier, actor Actor, id uuid.UUID) error {
	if actor.Role != RoleSeller && actor.Role != RoleAdmin {
		return apperr.AccessDenied()
	}
	current, err := s.Repo.GetForUpdate(ctx, q, id)
	if repository.IsNotFound(err) {
		return productNotFound(id)
	}
	if err != nil {
		return err
	}
	if actor.Role == RoleSeller && current.SellerID != actor.UserID {
		return apperr.AccessDenied()
	}
	return nil
}
