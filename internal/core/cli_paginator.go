package core

import "context"

// SetMaxPerPage ports Paginator::setMaxPerPage(): ALL_ITEMS (or any value below 1) returns every item in one page.
func (p *Paginator[T]) SetMaxPerPage(n int) {
	p.returnAll = n < 1
	if n >= 1 {
		p.maxPerPage = n
	}
	p.results = nil
}

// SetCurrentPage ports Pagerfanta::setCurrentPage() for pages that are known to exist.
func (p *Paginator[T]) SetCurrentPage(page int) {
	p.currentPage = page
	p.results = nil
}

// HasNextPage ports Pagerfanta::hasNextPage().
func (p *Paginator[T]) HasNextPage(ctx context.Context) (bool, error) {
	pages, err := p.NbPages(ctx)
	return p.currentPage < pages, err
}
