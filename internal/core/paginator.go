package core

import (
	"context"
	"fmt"
	"math"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// AllItems is Paginator::ALL_ITEMS.
const AllItems = -1

// PageAdapter is Pagerfanta's AdapterInterface.
type PageAdapter[T any] interface {
	Count(ctx context.Context) (int, error)
	Slice(ctx context.Context, offset, length int) ([]T, error)
}

// OutOfRangePageError ports Pagerfanta's OutOfRangeCurrentPageException, which upstream does not handle (it ends up as
// a server error).
type OutOfRangePageError struct{ Page, Pages int }

func (e OutOfRangePageError) Error() string {
	return fmt.Sprintf(`Page "%d" does not exist. The currentPage must be inferior to "%d"`, e.Page, e.Pages)
}

// Paginator ports shlink-common's Paginator on top of Pagerfanta.
type Paginator[T any] struct {
	adapter     PageAdapter[T]
	maxPerPage  int
	returnAll   bool
	currentPage int
	nbResults   *int
	results     []T
}

// NewPaginator mirrors `new Paginator($adapter)->setMaxPerPage($perPage)->setCurrentPage($page)`.
func NewPaginator[T any](ctx context.Context, adapter PageAdapter[T], perPage, page int) (*Paginator[T], error) {
	p := &Paginator[T]{adapter: adapter, maxPerPage: 10, currentPage: 1}
	p.returnAll = perPage < 1
	if perPage >= 1 {
		p.maxPerPage = perPage
	}
	if page < 1 {
		return nil, internal(fmt.Errorf("current page must be at least 1"))
	}
	if page > 1 {
		pages, err := p.NbPages(ctx)
		if err != nil {
			return nil, err
		}
		if page > pages {
			return nil, internal(OutOfRangePageError{page, pages})
		}
	}
	p.currentPage = page
	return p, nil
}

// NbResults returns the total amount of items.
func (p *Paginator[T]) NbResults(ctx context.Context) (int, error) {
	if p.nbResults == nil {
		n, err := p.adapter.Count(ctx)
		if err != nil {
			return 0, err
		}
		p.nbResults = &n
	}
	return *p.nbResults, nil
}

// MaxPerPage ports Paginator::getMaxPerPage().
func (p *Paginator[T]) MaxPerPage(ctx context.Context) (int, error) {
	if !p.returnAll {
		return p.maxPerPage, nil
	}
	n, err := p.NbResults(ctx)
	return max(n, 1), err
}

// NbPages ports Pagerfanta::getNbPages().
func (p *Paginator[T]) NbPages(ctx context.Context) (int, error) {
	n, err := p.NbResults(ctx)
	if err != nil {
		return 0, err
	}
	per, err := p.MaxPerPage(ctx)
	if err != nil {
		return 0, err
	}
	pages := int(math.Ceil(float64(n) / float64(per)))
	if pages == 0 {
		return 1, nil
	}
	return pages, nil
}

// CurrentPage returns the current page.
func (p *Paginator[T]) CurrentPage() int { return p.currentPage }

// Results returns the current page's items.
func (p *Paginator[T]) Results(ctx context.Context) ([]T, error) {
	if p.results == nil {
		per, err := p.MaxPerPage(ctx)
		if err != nil {
			return nil, err
		}
		items, err := p.adapter.Slice(ctx, (p.currentPage-1)*per, per)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []T{}
		}
		p.results = items
	}
	return p.results, nil
}

// Serialize ports PagerfantaUtils::serializePaginator().
func (p *Paginator[T]) Serialize(ctx context.Context, item func(T) any, dataProp string) (phpx.Map, error) {
	items, err := p.Results(ctx)
	if err != nil {
		return nil, err
	}
	data := make([]any, len(items))
	for i, it := range items {
		data[i] = item(it)
	}
	pages, err := p.NbPages(ctx)
	if err != nil {
		return nil, err
	}
	per, err := p.MaxPerPage(ctx)
	if err != nil {
		return nil, err
	}
	total, err := p.NbResults(ctx)
	if err != nil {
		return nil, err
	}
	return phpx.Map{
		{Key: dataProp, Value: data},
		{Key: "pagination", Value: phpx.Map{
			{Key: "currentPage", Value: p.currentPage},
			{Key: "pagesCount", Value: pages},
			{Key: "itemsPerPage", Value: per},
			{Key: "itemsInCurrentPage", Value: len(items)},
			{Key: "totalItems", Value: total},
		}},
	}, nil
}

// FuncAdapter builds an adapter from functions; the count is computed once like AbstractCacheableCountPaginatorAdapter.
type FuncAdapter[T any] struct {
	CountFn func(ctx context.Context) (int, error)
	SliceFn func(ctx context.Context, offset, length int) ([]T, error)
}

func (a FuncAdapter[T]) Count(ctx context.Context) (int, error) { return a.CountFn(ctx) }
func (a FuncAdapter[T]) Slice(ctx context.Context, offset, length int) ([]T, error) {
	return a.SliceFn(ctx, offset, length)
}
