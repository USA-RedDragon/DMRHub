package middleware

import (
	"math"
	"strconv"

	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/http/api/pagination"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PaginationConfig struct {
	DefaultLimit int
	MaxLimit     int
}

func PaginatedDatabaseProvider(db *gorm.DB, config PaginationConfig) gin.HandlerFunc {
	if config.MaxLimit == 0 {
		config.MaxLimit = 100
	}
	if config.DefaultLimit == 0 {
		config.DefaultLimit = 10
	}
	return func(c *gin.Context) {
		var limit int
		limitStr, exists := c.GetQuery("limit")
		if !exists {
			limit = config.DefaultLimit
		} else {
			if limitStr == "none" {
				limit = math.MaxInt32
			} else {
				var err error
				limit, err = strconv.Atoi(limitStr)
				if err != nil {
					// Bad limit, use default
					limit = config.DefaultLimit
				}
			}
		}

		if limitStr != "none" && limit > config.MaxLimit {
			limit = config.MaxLimit
		}
		if limit < 1 {
			limit = 1
		}

		var page int
		pageStr, exists := c.GetQuery("page")
		if !exists {
			page = 1
		} else {
			var err error
			page, err = strconv.Atoi(pageStr)
			if err != nil {
				// Bad page, use default
				page = 1
			}
		}

		if page < 1 {
			page = 1
		}

		c.Set("PaginatedDB",
			db.WithContext(c.Request.Context()).Scopes(pagination.NewPaginate(limit, page).Paginate),
		)
		c.Next()
	}
}
