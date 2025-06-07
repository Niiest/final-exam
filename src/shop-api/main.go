package main

import (
	"encoding/json"
	"example/shop/models"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var (
	products     []models.Product
	logMutex     sync.Mutex // Для безопасной записи в лог
	logFile      = "requests.json"
	productsFile = "products.json"
)

func main() {
	if err := loadProducts(productsFile); err != nil {
		log.Fatalf("Ошибка загрузки данных: %v", err)
	}

	r := chi.NewRouter()

	// Регистрация обработчиков
	r.Get("/products", searchProductsHandler)
	r.Get("/recommended", recommendedProductsHandler)

	// Запуск сервера
	log.Println("Сервер запущен на :8080")
	if err := http.ListenAndServe(":8080", logRequest(r)); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLog := models.RequestLog{
			Method:     r.Method,
			Path:       r.URL.Path,
			Query:      r.URL.RawQuery,
			RemoteAddr: r.RemoteAddr,
			Timestamp:  time.Now().UTC(),
			UserAgent:  r.UserAgent(),
		}

		go saveRequestLog(reqLog)

		next.ServeHTTP(w, r)
	})
}

func saveRequestLog(logEntry models.RequestLog) {
	logMutex.Lock()
	defer logMutex.Unlock()

	// Преобразуем в JSON
	data, err := json.Marshal(logEntry)
	if err != nil {
		log.Printf("Ошибка маршалинга лога: %v", err)
		return
	}

	data = append(data, '\n')

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Ошибка открытия файла лога: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		log.Printf("Ошибка записи в лог: %v", err)
	}
}

func loadProducts(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &products); err != nil {
		return err
	}

	log.Printf("Успешно загружено %d товаров", len(products))
	return nil
}

func searchProductsHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(r.URL.Query().Get("name"))
	if query == "" {
		respondWithError(w, http.StatusBadRequest, "Параметр 'name' обязателен")
		return
	}

	results := make([]models.Product, 0)
	for _, p := range products {
		if strings.Contains(strings.ToLower(p.Name), query) {
			results = append(results, p)
		}
	}

	if len(results) == 0 {
		respondWithError(w, http.StatusNotFound, "Товары не найдены")
		return
	}

	respondWithJSON(w, http.StatusOK, results)
}

func recommendedProductsHandler(w http.ResponseWriter, r *http.Request) {
	count := 5
	if c := r.URL.Query().Get("count"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			count = n
		}
	}

	recommended := getRecommended(count)
	respondWithJSON(w, http.StatusOK, recommended)
}

func getRecommended(count int) []models.Product {
	// Сортировка по дате обновления (сначала новые)
	sorted := make([]models.Product, len(products))
	copy(sorted, products)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt > sorted[j].UpdatedAt
	})

	if len(sorted) > count {
		return sorted[:count]
	}
	return sorted
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Ошибка кодирования JSON: %v", err)
	}
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}
