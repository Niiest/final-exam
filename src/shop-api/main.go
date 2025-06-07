package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"example/shop/models"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"github.com/lovoo/goka"
)

var (
	products     []models.Product
	logMutex     sync.Mutex // Для безопасной записи в лог
	logFile      = "requests.json"
	productsFile = "products.json"
	fileHandle   *os.File
	brokers      = []string{"localhost:9093", "localhost:9095", "localhost:9098"}
	logStream    = "requests"
	logEmitter   *goka.Emitter
)

func main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Ошибка загрузки файла .env")
		return
	}

	if err := loadProducts(productsFile); err != nil {
		log.Fatalf("Ошибка загрузки данных: %v", err)
	}

	openLogFile()
	setupGracefulShutdown()
	runEmitter()

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
		}

		go saveRequestLog(reqLog)

		next.ServeHTTP(w, r)
	})
}

func saveRequestLog(logEntry models.RequestLog) {
	go saveRequestLogToFile(&logEntry)
	saveRequestLogToKafka(&logEntry)
}

func saveRequestLogToKafka(logEntry *models.RequestLog) {
	key := logEntry.RemoteAddr + logEntry.Path + logEntry.Timestamp.UTC().Format(time.RFC3339)
	_, err := logEmitter.Emit(key, logEntry)
	if err != nil {
		log.Fatal(err)
	}
}

func saveRequestLogToFile(logEntry *models.RequestLog) {
	data, err := json.Marshal(*logEntry)
	if err != nil {
		log.Printf("Ошибка сериализации лога запроса: %v", err)
		return
	}

	data = append(data, '\n')

	logMutex.Lock()
	defer logMutex.Unlock()

	if _, err := fileHandle.Write(data); err != nil {
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

func runEmitter() {
	config := sarama.NewConfig()
	config.Net.SASL.Enable = true
	config.Net.SASL.User = os.Getenv("PRODUCER_USERNAME")
	config.Net.SASL.Password = os.Getenv("PRODUCER_PASSWORD")
	config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	config.Net.TLS.Enable = true

	caCertPool := x509.NewCertPool()
	if caCertPath := os.Getenv("KAFKA_CA_CERT_PATH"); caCertPath != "" {
		caCert, err := os.ReadFile(caCertPath)
		if err != nil {
			log.Fatal(err)
		}

		caCertPool.AppendCertsFromPEM([]byte(caCert))
	}

	config.Net.TLS.Config = &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: true,
	}

	config.Producer.Return.Successes = true
	config.Producer.RequiredAcks = sarama.WaitForAll

	e, err := goka.NewEmitter(
		brokers,
		goka.Stream(logStream),
		new(models.RequestLogCodec),
		goka.WithEmitterProducerBuilder(goka.ProducerBuilderWithConfig(config)),
	)
	if err != nil {
		log.Fatal(err)
	}

	logEmitter = e
}

func openLogFile() {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Ошибка открытия файла лога: %v", err)
		return
	}

	fileHandle = f
}

func setupGracefulShutdown() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGINT)
	go func() {
		sig := <-signalChan
		fmt.Println("Received signal:", sig)
		fileHandle.Close()
		fmt.Println("Log file handle closed")
		logEmitter.Finish()
		fmt.Println("Log emitter closed")
		os.Exit(0)
	}()
}
