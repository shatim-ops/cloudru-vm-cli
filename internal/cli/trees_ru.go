package cli

// The tree commands are made from the tools, whose descriptions are English:
// agents read them through MCP. The Russian help of a tree command takes its
// summary from here; a tool missing from the map falls back to the first
// sentence of its English description (TestTrees_RussianShorts keeps the map
// complete).

// treeGroupShortRU is the Russian summary of each tree.
var treeGroupShortRU = map[string]string{
	"vm":     "Виртуальные машины",
	"image":  "Образы ВМ: операционные системы и продукты маркетплейса",
	"disk":   "Диски",
	"vpc":    "Сети: VPC, маршруты, подсети, группы безопасности, floating IP",
	"k8s":    "Кластеры Managed Kubernetes",
	"pg":     "Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии",
	"s3":     "Object Storage: бакеты и объекты",
	"secret": "Secret Manager: секреты",
}

// toolShortRU is the Russian summary of each tool used by a tree command.
var toolShortRU = map[string]string{
	"vm_list":      "Список ВМ проекта: id, имя, состояние, зона, время создания",
	"vm_get":       "ВМ по ID или имени: внутренний и внешний IP, состояние, интерфейсы",
	"provision":    "Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip)",
	"destroy":      "Удалить ВМ и все данные на ней (безвозвратно)",
	"list_zones":   "Зоны доступности (UUID и имя)",
	"list_flavors": "Flavor ВМ: vCPU, RAM, бесплатный уровень, оценка цены за месяц",

	"image_list": "Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private)",
	"image_get":  "Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ",

	"disk_list":       "Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены",
	"list_disk_types": "Типы дисков (UUID и имя, например SSD или HDD)",
	"disk_create":     "Создать диск (с --attach-to — в зоне ВМ и сразу подключить)",
	"disk_attach":     "Подключить диск к ВМ (диск и ВМ должны быть в одной зоне)",
	"disk_detach":     "Отключить диск от ВМ",
	"disk_resize":     "Увеличить диск (только рост; затем расширьте раздел и файловую систему на ВМ)",
	"disk_delete":     "Удалить диск и все данные на нём (безвозвратно)",

	"vpc_list":             "Список VPC проекта",
	"vpc_get":              "VPC по ID: все сведения",
	"vpc_create":           "Создать VPC (асинхронно)",
	"vpc_delete":           "Удалить VPC (асинхронно; сначала удалите его подсети и ресурсы)",
	"vpc_operation":        "Состояние асинхронной операции VPC",
	"vpc_routes":           "Статические маршруты VPC",
	"vpc_route_create":     "Добавить статический маршрут в VPC (асинхронно)",
	"vpc_route_delete":     "Удалить статический маршрут из VPC (асинхронно)",
	"list_subnets":         "Подсети: UUID, имя, зона доступности",
	"list_security_groups": "Группы безопасности (с --rules — вместе с правилами)",
	"list_floating_ips":    "Floating IP: адрес, состояние, свободен ли",
	"fip_attach":           "Привязать свободный floating IP к основному интерфейсу ВМ",
	"release_floating_ip":  "Освободить floating IP (адрес, занятый ВМ, не освобождается)",

	"k8s_list":         "Кластеры Managed Kubernetes проекта",
	"k8s_get":          "Кластер Managed Kubernetes по ID",
	"k8s_options":      "Версии Kubernetes, flavor узлов и зоны для кластеров",
	"k8s_plan":         "Шаг 1: рассчитать конфигурацию кластера, ничего не создавая",
	"k8s_create":       "Шаг 2: создать кластер по расчёту k8s plan (платно, асинхронно, 10–20 минут)",
	"k8s_delete":       "Удалить кластер Managed Kubernetes (асинхронно)",
	"k8s_kubeconfig":   "kubeconfig кластера (по умолчанию со встроенным токеном примерно на час)",
	"k8s_nodepools":    "Группы узлов кластера",
	"k8s_add_nodepool": "Добавить группу рабочих узлов (платно, асинхронно)",
	"k8s_kubectl":      "Команда kubectl в кластере (изменяющие — с подтверждением)",
	"k8s_apply":        "Применить манифест Kubernetes (как kubectl apply -f -)",
	"k8s_helm":         "Helm в кластере: install, upgrade, uninstall, list, status",

	"pg_list":          "Кластеры Managed PostgreSQL проекта",
	"pg_get":           "Кластер PostgreSQL по ID: версия, хранилище, строка подключения, состояние",
	"pg_versions":      "Доступные версии PostgreSQL",
	"pg_specs":         "Конфигурации кластера PostgreSQL для версии: CPU, память, минимальный объём",
	"pg_create":        "Создать кластер PostgreSQL (платно, асинхронно)",
	"pg_delete":        "Удалить кластер PostgreSQL вместе с данными (асинхронно)",
	"pg_operation":     "Состояние асинхронной операции PostgreSQL",
	"pg_databases":     "Базы данных кластера",
	"pg_db_create":     "Создать базу данных",
	"pg_db_delete":     "Удалить базу данных",
	"pg_users":         "Пользователи кластера и их роли",
	"pg_user_create":   "Создать пользователя (пароль не выводится)",
	"pg_user_delete":   "Удалить пользователя",
	"pg_backups":       "Резервные копии кластера",
	"pg_backup_create": "Создать резервную копию вручную (асинхронно)",

	"s3_buckets":       "Бакеты Object Storage",
	"s3_ls":            "Объекты бакета: ключ, размер, время изменения",
	"s3_get":           "Скачать объект (небольшой текст — как есть, остальное — в base64)",
	"s3_put":           "Загрузить объект (существующий ключ перезаписывается)",
	"s3_rm":            "Удалить объект",
	"s3_bucket_create": "Создать бакет",
	"s3_bucket_delete": "Удалить пустой бакет",

	"secret_list":        "Секреты проекта (без значений)",
	"secret_get":         "Метаданные секрета: папка, метки, версии, ключ KMS (без значения)",
	"secret_value":       "Расшифрованное значение секрета",
	"secret_create":      "Создать секрет",
	"secret_add_version": "Добавить новую версию значения секрета (ротация)",
	"secret_delete":      "Удалить секрет со всеми версиями",
}
