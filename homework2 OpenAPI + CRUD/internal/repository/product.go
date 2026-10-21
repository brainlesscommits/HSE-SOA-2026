package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Product struct {
	ID          uuid.UUID
	Name        string
	Description *string
	Price       float64
	Stock       int
	Category    string
	Status      string
	SellerID    uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProductFilter struct {
	Status, Category string
	Page, Size       int
}

type ProductRepository struct{}

func (r ProductRepository) Create(ctx context.Context, q Querier, p Product) (Product, error) {
	var out Product
	err := q.QueryRow(ctx, `INSERT INTO products(id,name,description,price,stock,category,status,seller_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,name,description,price,stock,category,status,seller_id,created_at,updated_at`, p.ID, p.Name, p.Description, p.Price, p.Stock, p.Category, p.Status, p.SellerID).Scan(&out.ID, &out.Name, &out.Description, &out.Price, &out.Stock, &out.Category, &out.Status, &out.SellerID, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r ProductRepository) GetByID(ctx context.Context, q Querier, id uuid.UUID) (Product, error) {
	return scanProduct(q.QueryRow(ctx, `SELECT id,name,description,price,stock,category,status,seller_id,created_at,updated_at FROM products WHERE id=$1`, id))
}

func (r ProductRepository) List(ctx context.Context, q Querier, f ProductFilter) ([]Product, int, error) {
	args := []any{}
	where := "WHERE 1=1"
	if f.Status != "" {
		args = append(args, f.Status)
		where += " AND status=$" + itoa(len(args))
	}
	if f.Category != "" {
		args = append(args, f.Category)
		where += " AND category=$" + itoa(len(args))
	}
	var total int
	if err := q.QueryRow(ctx, "SELECT count(*) FROM products "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := f.Page * f.Size
	args = append(args, f.Size, offset)
	rows, err := q.Query(ctx, "SELECT id,name,description,price,stock,category,status,seller_id,created_at,updated_at FROM products "+where+" ORDER BY created_at DESC LIMIT $"+itoa(len(args)-1)+" OFFSET $"+itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var result []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, p)
	}
	return result, total, rows.Err()
}

func (r ProductRepository) Update(ctx context.Context, q Querier, p Product) (Product, error) {
	return scanProduct(q.QueryRow(ctx, `UPDATE products SET name=$2,description=$3,price=$4,stock=$5,category=$6,status=$7 WHERE id=$1 RETURNING id,name,description,price,stock,category,status,seller_id,created_at,updated_at`, p.ID, p.Name, p.Description, p.Price, p.Stock, p.Category, p.Status))
}

func (r ProductRepository) Archive(ctx context.Context, q Querier, id uuid.UUID) error {
	var exists bool
	err := q.QueryRow(ctx, `UPDATE products SET status='ARCHIVED' WHERE id=$1 RETURNING true`, id).Scan(&exists)
	return err
}

func (r ProductRepository) GetForUpdate(ctx context.Context, q Querier, id uuid.UUID) (Product, error) {
	return scanProduct(q.QueryRow(ctx, `SELECT id,name,description,price,stock,category,status,seller_id,created_at,updated_at FROM products WHERE id=$1 FOR UPDATE`, id))
}

func (r ProductRepository) UpdateStock(ctx context.Context, q Querier, id uuid.UUID, delta int) error {
	_, err := q.Exec(ctx, `UPDATE products SET stock=stock+$2 WHERE id=$1`, id, delta)
	return err
}

func scanProduct(row pgx.Row) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.Category, &p.Status, &p.SellerID, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
func itoa(n int) string { return strconv.Itoa(n) }
