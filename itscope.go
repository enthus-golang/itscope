package itscope

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type ITScopeCommunicator struct {
	username    string
	password    string
	userAgent   string
	language    Language
	client      *http.Client
	CompanyName string
	limiter     *rate.Limiter
}

func New(companyName string, userName string, password string, language Language) *ITScopeCommunicator {
	its := new(ITScopeCommunicator)
	its.CompanyName = companyName
	its.userAgent = its.CompanyName + "-ITS_ApiModule-0.1"
	its.username = userName
	its.password = password
	its.language = language
	its.client = &http.Client{}
	its.limiter = rate.NewLimiter(rate.Limit(6), 6)

	return its
}

func (its *ITScopeCommunicator) SetLanguage(language Language) {
	its.language = language
}

func (its *ITScopeCommunicator) authenticateRequest(request *http.Request) error {
	if its.username == "" || its.password == "" {
		return fmt.Errorf("no username or password set")
	}

	request.SetBasicAuth(its.username, its.password)
	request.Header.Add("Accept", "application/json")
	request.Header.Add("UserAgent", its.userAgent)
	request.Header.Add("Accept-Language", string(its.language))

	return nil
}

func (its *ITScopeCommunicator) GetProductData(ctx context.Context, productSKU string) (*Product, error) {
	productContainer, err := its.GetProductsFromQuery(ctx, "distpid="+productSKU)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve product data: %w", err)
	}

	if len(productContainer.Product) > 0 {
		return &productContainer.Product[0], nil
	} else {
		return nil, nil
	}
}

func (its *ITScopeCommunicator) GetAllProductTypes(ctx context.Context) ([]ProductType, error) {
	u := url.URL{
		Host:   "api.itscope.com",
		Scheme: "https",
		Path:   "2.0/products/producttypes/producttype.json",
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve product types: %w", err)
	}
	err = its.authenticateRequest(request)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve product types: %w", err)
	}

	retries := 3
	var response *http.Response
	for retries > 0 {
		if err = its.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("limiter timeout: %w", err)
		}

		response, err = its.client.Do(request)
		if err != nil || (response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound) {
			logrus.Errorln("Error during GetAllProductTypes, retrying...")
			time.Sleep(4 * time.Second)
			retries -= 1
		} else {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("could not retrieve product types: %w", err)
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close response body: %w", cerr)
		}
	}()

	if response.StatusCode == http.StatusNotFound {
		return []ProductType{}, nil
	} else if response.StatusCode != http.StatusOK {
		return nil, NewUnexpectedStatusCodeError(response)
	}
	var productTypes ProductTypesContainer
	err = json.NewDecoder(response.Body).Decode(&productTypes)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve product types: %w", err)
	}

	return productTypes.ProductTypes, nil
}

func (its *ITScopeCommunicator) GetProductAccessoriesFromList(ctx context.Context, products []string) ([]Product, error) {
	if len(products) == 0 {
		return nil, nil
	}

	productList := make([]Product, 0)

	queryStrings := its.createQueryStrings(products, 50)

	for _, query := range queryStrings {
		query := query

		product, err := its.GetProductsFromQuery(ctx, query)
		if err != nil {
			return nil, err
		}

		productList = append(productList, product.Product...)

	}

	return productList, nil
}

func (its *ITScopeCommunicator) GetProductsFromQuery(ctx context.Context, query string) (*ProductsContainer, error) {
	urlString := "https://api.itscope.com/2.0/products/search/" + url.QueryEscape(query) + "/standard.json?realtime=false&plzproducts=false&page=1&item=0&sort=DEFAULT"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, urlString, nil)
	if err != nil {
		return nil, fmt.Errorf("GetProductsFromQuery1: %w", err)
	}
	err = its.authenticateRequest(request)
	if err != nil {
		return nil, fmt.Errorf("GetProductsFromQuery2: %w", err)
	}

	retries := 3
	var response *http.Response
	for retries > 0 {
		if err = its.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("limiter timeout: %w", err)
		}

		response, err = its.client.Do(request)
		if err != nil || (response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound) {
			logrus.Errorln("Error during GetProductsFromQuery, retrying...")
			retries -= 1
			time.Sleep(4 * time.Second)
		} else {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("GetProductsFromQuery: %w", err)
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close response body: %w", cerr)
		}
	}()

	if response.StatusCode == http.StatusNotFound {
		return &ProductsContainer{}, nil
	} else if response.StatusCode != http.StatusOK {
		return nil, NewUnexpectedStatusCodeError(response)
	}
	var products ProductsContainer
	err = json.NewDecoder(response.Body).Decode(&products)
	if err != nil {
		return nil, err
	}

	return &products, nil
}

func (its *ITScopeCommunicator) createQueryStrings(productIDs []string, length int) []string {
	var requestQuerys = make([]string, 0)
	var pages = int(len(productIDs) / length)

	if len(productIDs)%length > 0 {
		pages = pages + 1
	}

	for i := pages; i > 0; i-- {
		start := (i - 1) * length
		end := (i) * length
		var slice []string
		if i == pages {
			slice = productIDs[start:]
		} else {
			slice = productIDs[start:end]
		}
		var query = "id=" + strings.Join(slice, ";id=")
		requestQuerys = append(requestQuerys, query)
	}

	return requestQuerys
}

func (its *ITScopeCommunicator) GetProductImages(product *Product) []string {
	var imageUrls = make([]string, 0)
	if product.Image1 != "" {
		imageUrls = append(imageUrls, product.Image1)
	}
	if product.Image2 != "" {
		imageUrls = append(imageUrls, product.Image2)
	}
	if product.Image3 != "" {
		imageUrls = append(imageUrls, product.Image3)
	}
	if product.Image4 != "" {
		imageUrls = append(imageUrls, product.Image4)
	}
	if product.Image5 != "" {
		imageUrls = append(imageUrls, product.Image5)
	}

	return imageUrls
}

func (its *ITScopeCommunicator) FilterProductTypesByGroupId(groupId string, productTypes []ProductType) []ProductType {
	filteredTypes := make([]ProductType, 0)

	for _, productType := range productTypes {
		if productType.ProductTypeGroup.ID == groupId {
			filteredTypes = append(filteredTypes, productType)
		}
	}

	return filteredTypes
}

func (its *ITScopeCommunicator) FilterProductTypesById(id string, productTypes []ProductType) []ProductType {
	filteredTypes := make([]ProductType, 0)

	for _, productType := range productTypes {
		if productType.ID == id {
			filteredTypes = append(filteredTypes, productType)
		}
	}

	return filteredTypes
}

func (its *ITScopeCommunicator) FilterProductsByTypeList(products []Product, typeList []ProductType) []Product {
	filteredProducts := make(map[string]Product)

	for _, productType := range typeList {
		for _, product := range products {
			if product.ProductTypeID == productType.ID && product.ProductTypeID != "" && productType.ProductTypeGroup.ID != "" {
				filteredProducts[product.Puid] = product
			}
		}
	}

	filteredProductsArray := make([]Product, len(filteredProducts))
	iterator := 0

	for _, item := range filteredProducts {
		filteredProductsArray[iterator] = item
		iterator++
	}

	return filteredProductsArray
}

func (its *ITScopeCommunicator) GetServiceTypeAccessoriesOfProduct(ctx context.Context, product *Product) ([]Product, error) {
	productTypes, err := its.GetAllProductTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetServiceTypeAccessoriesOfProduct: %w", err)
	}

	accessories, err := its.GetProductAccessories(ctx, product)
	if err != nil {
		return nil, fmt.Errorf("GetServiceTypeAccessoriesOfProduct: %w", err)
	}

	serviceTypes := its.FilterProductTypesByGroupId("SSP", productTypes)
	return its.FilterProductsByTypeList(accessories, serviceTypes), nil
}

func (its *ITScopeCommunicator) GetProductAccessories(ctx context.Context, product *Product) ([]Product, error) {
	accessoryIds := make([]string, len(product.Accessories))
	for i, v := range product.Accessories {
		accessoryIds[i] = v.ReferencedProductID
	}

	accessories, err := its.GetProductAccessoriesFromList(ctx, accessoryIds)
	if err != nil {
		return nil, fmt.Errorf("GetProductAccessories: %w", err)
	}

	return accessories, nil
}
