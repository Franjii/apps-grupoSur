package models

const PageSize = 50

type Pagination struct {
	Page       int
	TotalItems int
	PageSize   int
}

func NewPagination(page, totalItems int) Pagination {
	return Pagination{
		Page:       page,
		TotalItems: totalItems,
		PageSize:   PageSize,
	}
}

func (p Pagination) TotalPages() int {
	if p.TotalItems == 0 {
		return 1
	}
	pages := p.TotalItems / p.PageSize
	if p.TotalItems%p.PageSize > 0 {
		pages++
	}
	return pages
}

func (p Pagination) HasPrev() bool {
	return p.Page > 1
}

func (p Pagination) HasNext() bool {
	return p.Page < p.TotalPages()
}

func (p Pagination) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// PageRange returns up to 7 page numbers for navigation.
// A 0 value is a sentinel for ellipsis ("...").
func (p Pagination) PageRange() []int {
	total := p.TotalPages()
	if total <= 7 {
		r := make([]int, total)
		for i := range r {
			r[i] = i + 1
		}
		return r
	}

	// Always show first, last, current, and neighbors
	current := p.Page
	var r []int

	r = append(r, 1)

	if current > 3 {
		r = append(r, 0) // ellipsis
	}

	start := current - 1
	end := current + 1
	if start < 2 {
		start = 2
	}
	if end > total-1 {
		end = total - 1
	}

	for i := start; i <= end; i++ {
		r = append(r, i)
	}

	if current < total-2 {
		r = append(r, 0) // ellipsis
	}

	r = append(r, total)

	return r
}
