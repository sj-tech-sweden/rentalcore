package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"go-barcode-webapp/internal/models"
	"go-barcode-webapp/internal/repository"
	"go-barcode-webapp/internal/services"

	"github.com/gin-gonic/gin"
)

type CustomerHandler struct {
	customerRepo  *repository.CustomerRepository
	twentyService *services.TwentyService
}

func NewCustomerHandler(customerRepo *repository.CustomerRepository) *CustomerHandler {
	return &CustomerHandler{customerRepo: customerRepo}
}

// SetTwentyService injects the Twenty CRM service into the handler.
func (h *CustomerHandler) SetTwentyService(svc *services.TwentyService) {
	h.twentyService = svc
}

func (h *CustomerHandler) ListCustomers(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	params := &models.FilterParams{}
	if err := c.ShouldBindQuery(params); err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": err.Error(), "user": user})
		return
	}

	// Manual parameter extraction to ensure search works
	searchParam := c.Query("search")
	if searchParam != "" {
		params.SearchTerm = searchParam
	}

	customers, err := h.customerRepo.List(params)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error(), "user": user})
		return
	}

	c.HTML(http.StatusOK, "customers.html", gin.H{
		"title":       "Customers",
		"customers":   customers,
		"params":      params,
		"user":        user,
		"currentPage": "customers",
	})
}

func (h *CustomerHandler) NewCustomerForm(c *gin.Context) {
	// Only allow fetch requests from modals, block direct browser access
	acceptHeader := c.GetHeader("Accept")
	xRequestedWith := c.GetHeader("X-Requested-With")

	// Block direct browser access - only allow modal/fetch requests
	if xRequestedWith != "XMLHttpRequest" && !strings.Contains(acceptHeader, "application/json") && !strings.Contains(acceptHeader, "text/html") {
		c.Redirect(http.StatusFound, "/customers")
		return
	}

	// If it's a direct browser request (Accept: text/html without XMLHttpRequest), redirect
	if strings.Contains(acceptHeader, "text/html") && xRequestedWith != "XMLHttpRequest" {
		c.Redirect(http.StatusFound, "/customers")
		return
	}

	user, _ := GetCurrentUser(c)

	c.HTML(http.StatusOK, "customer_form.html", gin.H{
		"title":    "New Customer",
		"customer": &models.Customer{},
		"user":     user,
	})
}

func (h *CustomerHandler) CreateCustomer(c *gin.Context) {
	// Parse form first
	c.Request.ParseForm()

	companyName := c.PostForm("company_name")
	firstName := c.PostForm("first_name")
	lastName := c.PostForm("last_name")
	email := c.PostForm("email")
	phoneNumber := c.PostForm("phone_number")
	street := c.PostForm("street")
	houseNumber := c.PostForm("house_number")
	zip := c.PostForm("zip")
	city := c.PostForm("city")
	federalState := c.PostForm("federal_state")
	country := c.PostForm("country")
	customerType := c.PostForm("customer_type")
	notes := c.PostForm("notes")

	customer := models.Customer{
		CompanyName:  &companyName,
		FirstName:    &firstName,
		LastName:     &lastName,
		Email:        &email,
		PhoneNumber:  &phoneNumber,
		Street:       &street,
		HouseNumber:  &houseNumber,
		ZIP:          &zip,
		City:         &city,
		FederalState: &federalState,
		Country:      &country,
		CustomerType: &customerType,
		Notes:        &notes,
	}

	if err := h.customerRepo.Create(&customer); err != nil {
		user, _ := GetCurrentUser(c)
		c.HTML(http.StatusInternalServerError, "customer_form.html", gin.H{
			"title":    "New Customer",
			"customer": &customer,
			"error":    err.Error(),
			"user":     user,
		})
		return
	}

	if h.twentyService != nil {
		h.twentyService.SyncCustomerAsync(&customer)
	}

	c.Redirect(http.StatusSeeOther, "/customers")
}

func (h *CustomerHandler) GetCustomer(c *gin.Context) {
	// Only allow fetch requests from modals, block direct browser access
	acceptHeader := c.GetHeader("Accept")
	xRequestedWith := c.GetHeader("X-Requested-With")

	// Block direct browser access - only allow modal/fetch requests
	if xRequestedWith != "XMLHttpRequest" && !strings.Contains(acceptHeader, "application/json") && !strings.Contains(acceptHeader, "text/html") {
		c.Redirect(http.StatusFound, "/customers")
		return
	}

	// If it's a direct browser request (Accept: text/html without XMLHttpRequest), redirect
	if strings.Contains(acceptHeader, "text/html") && xRequestedWith != "XMLHttpRequest" {
		c.Redirect(http.StatusFound, "/customers")
		return
	}

	user, _ := GetCurrentUser(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "Invalid customer ID", "user": user})
		return
	}

	customer, err := h.customerRepo.GetByID(uint(id))
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Customer not found", "user": user})
		return
	}

	c.HTML(http.StatusOK, "customer_detail.html", gin.H{
		"customer": customer,
		"user":     user,
	})
}

func (h *CustomerHandler) EditCustomerForm(c *gin.Context) {
	// Only allow fetch requests from modals, block direct browser access
	acceptHeader := c.GetHeader("Accept")
	xRequestedWith := c.GetHeader("X-Requested-With")

	// Block direct browser access - only allow modal/fetch requests
	if xRequestedWith != "XMLHttpRequest" && !strings.Contains(acceptHeader, "application/json") && !strings.Contains(acceptHeader, "text/html") {
		c.Redirect(http.StatusFound, "/customers")
		return
	}

	// If it's a direct browser request (Accept: text/html without XMLHttpRequest), redirect
	if strings.Contains(acceptHeader, "text/html") && xRequestedWith != "XMLHttpRequest" {
		c.Redirect(http.StatusFound, "/customers")
		return
	}

	user, _ := GetCurrentUser(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "Invalid customer ID", "user": user})
		return
	}

	customer, err := h.customerRepo.GetByID(uint(id))
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Customer not found", "user": user})
		return
	}

	c.HTML(http.StatusOK, "customer_form.html", gin.H{
		"title":    "Edit Customer",
		"customer": customer,
		"user":     user,
	})
}

func (h *CustomerHandler) UpdateCustomer(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "Invalid customer ID", "user": user})
		return
	}

	companyName := c.PostForm("company_name")
	firstName := c.PostForm("first_name")
	lastName := c.PostForm("last_name")
	email := c.PostForm("email")
	phoneNumber := c.PostForm("phone_number")
	street := c.PostForm("street")
	houseNumber := c.PostForm("house_number")
	zip := c.PostForm("zip")
	city := c.PostForm("city")
	federalState := c.PostForm("federal_state")
	country := c.PostForm("country")
	customerType := c.PostForm("customer_type")
	notes := c.PostForm("notes")

	customer := models.Customer{
		CustomerID:   uint(id),
		CompanyName:  &companyName,
		FirstName:    &firstName,
		LastName:     &lastName,
		Email:        &email,
		PhoneNumber:  &phoneNumber,
		Street:       &street,
		HouseNumber:  &houseNumber,
		ZIP:          &zip,
		City:         &city,
		FederalState: &federalState,
		Country:      &country,
		CustomerType: &customerType,
		Notes:        &notes,
	}

	if err := h.customerRepo.Update(&customer); err != nil {
		c.HTML(http.StatusInternalServerError, "customer_form.html", gin.H{
			"title":    "Edit Customer",
			"customer": &customer,
			"error":    err.Error(),
			"user":     user,
		})
		return
	}

	c.Redirect(http.StatusFound, "/customers")
}

func (h *CustomerHandler) DeleteCustomer(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid customer ID"})
		return
	}

	if err := h.customerRepo.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Customer deleted successfully"})
}

// API handlers
// ListCustomersAPI godoc
// @Summary      List customers
// @Description  Returns a paginated list of customers
// @Tags         customers
// @Produce      json
// @Param        search    query    string  false  "Search term"
// @Param        page      query    int     false  "Page number"
// @Param        pageSize  query    int     false  "Page size"
// @Success      200  {object}  map[string]interface{}  "List of customers"
// @Failure      400  {object}  map[string]string       "Invalid request"
// @Failure      500  {object}  map[string]string       "Internal server error"
// @Security     SessionCookie
// @Router       /customers [get]
func (h *CustomerHandler) ListCustomersAPI(c *gin.Context) {
	params := &models.FilterParams{}
	if err := c.ShouldBindQuery(params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	customers, err := h.customerRepo.List(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"customers": customers})
}

// CreateCustomerAPI godoc
// @Summary      Create a customer
// @Description  Creates a new customer
// @Tags         customers
// @Accept       json
// @Produce      json
// @Param        customer  body      models.Customer         true  "Customer data"
// @Success      201       {object}  models.Customer         "Created customer"
// @Failure      400       {object}  map[string]string       "Invalid request"
// @Failure      500       {object}  map[string]string       "Internal server error"
// @Security     SessionCookie
// @Router       /customers [post]
func (h *CustomerHandler) CreateCustomerAPI(c *gin.Context) {
	var customer models.Customer
	if err := c.ShouldBindJSON(&customer); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.customerRepo.Create(&customer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.twentyService != nil {
		h.twentyService.SyncCustomerAsync(&customer)
	}

	c.JSON(http.StatusCreated, customer)
}

// GetCustomerAPI godoc
// @Summary      Get a customer
// @Description  Returns details of a specific customer by ID
// @Tags         customers
// @Produce      json
// @Param        id   path      int                     true  "Customer ID"
// @Success      200  {object}  models.Customer         "Customer details"
// @Failure      400  {object}  map[string]string       "Invalid ID"
// @Failure      404  {object}  map[string]string       "Customer not found"
// @Security     SessionCookie
// @Router       /customers/{id} [get]
func (h *CustomerHandler) GetCustomerAPI(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid customer ID"})
		return
	}

	customer, err := h.customerRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Customer not found"})
		return
	}

	c.JSON(http.StatusOK, customer)
}

// UpdateCustomerAPI godoc
// @Summary      Update a customer
// @Description  Updates an existing customer
// @Tags         customers
// @Accept       json
// @Produce      json
// @Param        id        path      int                     true  "Customer ID"
// @Param        customer  body      models.Customer         true  "Customer update data"
// @Success      200       {object}  models.Customer         "Updated customer"
// @Failure      400       {object}  map[string]string       "Invalid request"
// @Failure      500       {object}  map[string]string       "Internal server error"
// @Security     SessionCookie
// @Router       /customers/{id} [put]
func (h *CustomerHandler) UpdateCustomerAPI(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid customer ID"})
		return
	}

	var customer models.Customer
	if err := c.ShouldBindJSON(&customer); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	customer.CustomerID = uint(id)
	if err := h.customerRepo.Update(&customer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.twentyService != nil {
		h.twentyService.SyncCustomerAsync(&customer)
	}

	c.JSON(http.StatusOK, customer)
}

// DeleteCustomerAPI godoc
// @Summary      Delete a customer
// @Description  Deletes a customer by ID
// @Tags         customers
// @Produce      json
// @Param        id   path      int                     true  "Customer ID"
// @Success      200  {object}  map[string]string       "Success message"
// @Failure      400  {object}  map[string]string       "Invalid ID"
// @Failure      500  {object}  map[string]string       "Internal server error"
// @Security     SessionCookie
// @Router       /customers/{id} [delete]
func (h *CustomerHandler) DeleteCustomerAPI(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid customer ID"})
		return
	}

	if err := h.customerRepo.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Customer deleted successfully"})
}
