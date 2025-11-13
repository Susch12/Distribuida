# Diagramas de Arquitectura Detallados
## Productor-Consumidor vs Publisher-Subscriber

## Índice
1. [Visión General Comparativa](#1-visión-general-comparativa)
2. [Flujo de Datos Detallado](#2-flujo-de-datos-detallado)
3. [Secuencia de Operaciones](#3-secuencia-de-operaciones)
4. [Arquitectura de Componentes](#4-arquitectura-de-componentes)
5. [Patrones de Comunicación](#5-patrones-de-comunicación)
6. [Manejo de Errores y Reintentos](#6-manejo-de-errores-y-reintentos)

---

## 1. Visión General Comparativa

### Modelo Productor-Consumidor

```mermaid
graph TB
    subgraph "MODELO PRODUCTOR-CONSUMIDOR"
        subgraph "Servidor"
            P[Productor<br/>Genera trabajos únicos]
            Q[Cola FIFO Única<br/>Buffer: 10,000]
            S[Estadísticas<br/>Mutex Protected]
            
            P -->|Produce| Q
        end
        
        subgraph "Características Clave"
            C1[✓ Un trabajo = Un cliente]
            C2[✓ Distribución automática]
            C3[✓ Sin duplicados]
            C4[✓ Orden FIFO garantizado]
        end
        
        subgraph "Clientes Consumidores"
            CL1[Cliente 1<br/>Consume y procesa]
            CL2[Cliente 2<br/>Consume y procesa]
            CL3[Cliente 3<br/>Consume y procesa]
            CLN[Cliente N<br/>Consume y procesa]
        end
        
        Q -->|Trabajo A| CL1
        Q -->|Trabajo B| CL2
        Q -->|Trabajo C| CL3
        Q -->|Trabajo D| CLN
        
        CL1 -->|Resultado A| S
        CL2 -->|Resultado B| S
        CL3 -->|Resultado C| S
        CLN -->|Resultado D| S
    end
    
    style P fill:#90EE90
    style Q fill:#FFD700
    style S fill:#DDA0DD
    style CL1 fill:#87CEEB
    style CL2 fill:#87CEEB
    style CL3 fill:#87CEEB
    style CLN fill:#87CEEB
```

### Modelo Publisher-Subscriber

```mermaid
graph TB
    subgraph "MODELO PUBLISHER-SUBSCRIBER"
        subgraph "Servidor"
            PUB[Publisher<br/>Genera eventos]
            R[Router<br/>Selección de cola]
            
            subgraph "Colas por Tema"
                Q1[Cola Primary<br/>50% mensajes]
                Q2[Cola Secondary<br/>30% mensajes]
                Q3[Cola Tertiary<br/>20% mensajes]
            end
            
            ST[Estadísticas<br/>Por cliente y cola]
            
            PUB --> R
            R -->|Criterio| Q1
            R -->|Criterio| Q2
            R -->|Criterio| Q3
        end
        
        subgraph "Características Clave"
            F1[✓ Un mensaje = N clientes]
            F2[✓ Suscripción por tema]
            F3[✓ Permite duplicados]
            F4[✓ Desacoplamiento]
        end
        
        subgraph "Clientes Suscriptores"
            S1[Suscriptor 1<br/>Primary]
            S2[Suscriptor 2<br/>Primary + Secondary]
            S3[Suscriptor 3<br/>Tertiary]
            S4[Suscriptor 4<br/>Secondary]
        end
        
        Q1 -.->|Stream| S1
        Q1 -.->|Stream| S2
        Q2 -.->|Stream| S2
        Q2 -.->|Stream| S4
        Q3 -.->|Stream| S3
        
        S1 -->|Resultados| ST
        S2 -->|Resultados| ST
        S3 -->|Resultados| ST
        S4 -->|Resultados| ST
    end
    
    style PUB fill:#90EE90
    style R fill:#FFA500
    style Q1 fill:#FFD700
    style Q2 fill:#FFD700
    style Q3 fill:#FFD700
    style ST fill:#DDA0DD
    style S1 fill:#87CEEB
    style S2 fill:#87CEEB
    style S3 fill:#87CEEB
    style S4 fill:#87CEEB
```

---

## 2. Flujo de Datos Detallado

### Productor-Consumidor: Flujo Completo

```mermaid
sequenceDiagram
    participant Prod as Productor
    participant Hash as Hash Map<br/>(Unicidad)
    participant Queue as Cola FIFO
    participant C1 as Cliente 1
    participant C2 as Cliente 2
    participant Stats as Estadísticas
    
    Note over Prod: Genera vector [n1, n2, n3]
    Prod->>Hash: ¿Vector único?
    
    alt Vector ya existe
        Hash-->>Prod: Duplicado ❌
        Prod->>Prod: Regenera vector
    else Vector nuevo
        Hash-->>Prod: Único ✓
        Prod->>Queue: Encola vector
        Note over Queue: Vec añadido al final
    end
    
    Note over C1,C2: Clientes compiten por trabajos
    
    C1->>Queue: GetNumbers()
    Queue-->>C1: Vector A [1,2,3]
    Note over C1: Solo C1 recibe Vector A
    
    C2->>Queue: GetNumbers()
    Queue-->>C2: Vector B [4,5,6]
    Note over C2: Solo C2 recibe Vector B
    
    Note over C1: Procesa: 1+2+3 = 6
    C1->>Stats: SubmitResult(6)
    Stats->>Stats: totalResults++<br/>clientStats[C1]++<br/>sum += 6
    Stats-->>C1: Success ✓
    
    Note over C2: Procesa: 4+5+6 = 15
    C2->>Stats: SubmitResult(15)
    Stats->>Stats: totalResults++<br/>clientStats[C2]++<br/>sum += 15
    Stats-->>C2: Success ✓
    
    Note over Stats: Si totalResults >= 1M<br/>Sistema se detiene
```

### Publisher-Subscriber: Flujo Completo

```mermaid
sequenceDiagram
    participant Pub as Publisher
    participant Router as Router
    participant Q1 as Cola Primary
    participant Q2 as Cola Secondary
    participant S1 as Suscriptor 1<br/>(Primary)
    participant S2 as Suscriptor 2<br/>(Primary+Secondary)
    participant Stats as Estadísticas
    
    Note over Pub: Genera set [n1, n2, n3]
    Pub->>Router: Selecciona cola
    
    alt Criterio: Aleatorio
        Router->>Router: Random(33/33/33%)
    else Criterio: Ponderado
        Router->>Router: Weighted(50/30/20%)
    else Criterio: Condicional
        Router->>Router: EvenOdd(n1,n2,n3)
    end
    
    Router->>Q1: Publica en Primary
    Note over Q1: Mensaje disponible
    
    par Broadcast a suscriptores de Primary
        Q1-->>S1: Stream: Msg [10,20,30]
        Q1-->>S2: Stream: Msg [10,20,30]
    end
    
    Note over S1,S2: Ambos reciben MISMO mensaje
    
    Note over S1: Procesa: 10+20+30 = 60
    S1->>Stats: SendResult(60)
    Stats->>Stats: clientResults[S1] += 60
    Stats-->>S1: Success ✓
    
    Note over S2: También procesa: 10+20+30 = 60
    S2->>Stats: SendResult(60)
    Stats->>Stats: clientResults[S2] += 60
    Stats-->>S2: Success ✓
    
    Note over Stats: Mismo mensaje procesado 2 veces<br/>por diferentes suscriptores
    
    Router->>Q2: Publica en Secondary
    Q2-->>S2: Stream: Msg [5,15,25]
    Note over S2: Solo S2 está suscrito
    
    S2->>Stats: SendResult(45)
```

---

## 3. Secuencia de Operaciones

### Productor-Consumidor: Ciclo de Vida Completo

```mermaid
stateDiagram-v2
    [*] --> Inicialización
    
    Inicialización --> ProduccionActiva: Servidor inicia
    
    state ProduccionActiva {
        [*] --> GenerarVector
        GenerarVector --> VerificarUnicidad
        VerificarUnicidad --> Duplicado: Ya existe
        VerificarUnicidad --> Nuevo: Único
        Duplicado --> GenerarVector
        Nuevo --> IntentarEncolar
        IntentarEncolar --> Encolado: Cola disponible
        IntentarEncolar --> EsperarEspacio: Cola llena
        EsperarEspacio --> IntentarEncolar: Timeout 100ms
        Encolado --> GenerarVector
    }
    
    state ConsumoActivo {
        [*] --> EsperandoCliente
        EsperandoCliente --> ConsumirVector: GetNumbers()
        ConsumirVector --> Procesando
        Procesando --> EnviarResultado
        EnviarResultado --> ActualizarStats
        ActualizarStats --> VerificarLimite
        VerificarLimite --> EsperandoCliente: < 1M
        VerificarLimite --> LimiteAlcanzado: >= 1M
    }
    
    ProduccionActiva --> ConsumoActivo: Clientes conectan
    
    LimiteAlcanzado --> Finalizando
    
    state Finalizando {
        [*] --> DetencionProductor
        DetencionProductor --> CerrarCola
        CerrarCola --> NotificarClientes
        NotificarClientes --> GenerarReporte
        GenerarReporte --> [*]
    }
    
    Finalizando --> [*]
```

### Publisher-Subscriber: Ciclo de Vida Completo

```mermaid
stateDiagram-v2
    [*] --> Inicialización
    
    Inicialización --> PublicacionActiva: Servidor inicia
    
    state PublicacionActiva {
        [*] --> GenerarSet
        GenerarSet --> SeleccionarCola
        
        state SeleccionarCola {
            [*] --> EvaluarCriterio
            EvaluarCriterio --> Primary: 50% o condición
            EvaluarCriterio --> Secondary: 30% o condición
            EvaluarCriterio --> Tertiary: 20% o condición
        }
        
        Primary --> PublicarMensaje
        Secondary --> PublicarMensaje
        Tertiary --> PublicarMensaje
        PublicarMensaje --> GenerarSet
    }
    
    state SuscripcionActiva {
        [*] --> ClienteConecta
        ClienteConecta --> ElegirColas: Subscribe()
        
        state ElegirColas {
            [*] --> Una: 50% probabilidad
            [*] --> Dos: 50% probabilidad
        }
        
        Una --> EscucharCola
        Dos --> EscucharDosColas
        
        state EscucharCola {
            [*] --> EsperarMensaje
            EsperarMensaje --> RecibirMensaje
            RecibirMensaje --> Procesar
            Procesar --> EnviarResultado
            EnviarResultado --> EsperarMensaje
        }
        
        state EscucharDosColas {
            [*] --> EsperarAmbas
            EsperarAmbas --> RecibirDeCola1: Msg en cola 1
            EsperarAmbas --> RecibirDeCola2: Msg en cola 2
            RecibirDeCola1 --> ProcesarYEnviar
            RecibirDeCola2 --> ProcesarYEnviar
            ProcesarYEnviar --> EsperarAmbas
        }
    }
    
    PublicacionActiva --> SuscripcionActiva: Cliente subscribe
    
    SuscripcionActiva --> Verificando: Cada resultado
    
    state Verificando {
        [*] --> Contador
        Contador --> ContinuarPublicando: < 1M
        Contador --> DetenerPublicacion: >= 1M
    }
    
    ContinuarPublicando --> PublicacionActiva
    DetenerPublicacion --> ReporteFinal
    
    ReporteFinal --> [*]
```

---

## 4. Arquitectura de Componentes

### Productor-Consumidor: Arquitectura Interna

```mermaid
graph TB
    subgraph "SERVIDOR - Arquitectura Interna"
        subgraph "Capa de Producción"
            PG[Productor Goroutine]
            RNG[Generador Random]
            HM[Hash Map<br/>Vectores Únicos]
            VM[vectorMutex]
            
            PG --> RNG
            PG --> HM
            HM --> VM
        end
        
        subgraph "Capa de Cola"
            BC[Buffered Channel<br/>chan Vector<br/>cap: 10,000]
            QM[queueMutex<br/>RWMutex]
            QC[queueClosed<br/>bool]
            
            BC --> QM
            QC --> QM
        end
        
        subgraph "Capa RPC"
            GN[GetNumbers Handler]
            SR[SubmitResult Handler]
            
            GN --> BC
            SR --> STATS
        end
        
        subgraph "Capa de Estadísticas"
            STATS[Stats Manager]
            SM[statsMutex<br/>RWMutex]
            TR[totalResults: int64]
            RS[resultSum: int64]
            CS[clientStats: map]
            
            STATS --> SM
            SM --> TR
            SM --> RS
            SM --> CS
        end
        
        subgraph "Capa de Control"
            SS[systemStopped: bool]
            STM[stopMutex<br/>RWMutex]
            SC[stopChan]
            
            SS --> STM
            SC --> STM
        end
        
        PG -->|produce| BC
        TR -.->|>= 1M| SS
    end
    
    subgraph "CLIENTE - Arquitectura Interna"
        subgraph "Conexión"
            CONN[gRPC Connection]
            KA[Keep-Alive<br/>10s ping]
            
            CONN --> KA
        end
        
        subgraph "Loop Principal"
            REQ[Request Numbers]
            PROC[Procesar<br/>suma(n1,n2,n3)]
            SEND[Send Result]
            
            REQ --> PROC
            PROC --> SEND
            SEND --> REQ
        end
        
        subgraph "Control de Errores"
            ERR[Error Counter]
            RETRY[Retry Logic]
            MAX[Max Failures: 5]
            
            ERR --> MAX
            MAX --> RETRY
        end
        
        CONN --> REQ
        SEND -.->|error| ERR
    end
    
    style PG fill:#90EE90
    style BC fill:#FFD700
    style STATS fill:#DDA0DD
    style SS fill:#FF6B6B
```

### Publisher-Subscriber: Arquitectura Interna

```mermaid
graph TB
    subgraph "SERVIDOR - Arquitectura Interna"
        subgraph "Capa de Publicación"
            PUBG[Publisher Goroutine]
            TICKER[Ticker<br/>50ms]
            MSGID[Message ID<br/>Counter]
            MIM[messageIDMu]
            
            PUBG --> TICKER
            PUBG --> MSGID
            MSGID --> MIM
        end
        
        subgraph "Capa de Routing"
            ROUTER[Router]
            CRIT[Criterio Selección]
            
            subgraph "Criterios"
                ALE[Aleatorio<br/>33/33/33%]
                POND[Ponderado<br/>50/30/20%]
                COND[Condicional<br/>par/impar]
            end
            
            ROUTER --> CRIT
            CRIT --> ALE
            CRIT --> POND
            CRIT --> COND
        end
        
        subgraph "Capa de Colas"
            Q1[Primary Queue<br/>chan *NumberSet<br/>cap: 1,000]
            Q2[Secondary Queue<br/>chan *NumberSet<br/>cap: 1,000]
            Q3[Tertiary Queue<br/>chan *NumberSet<br/>cap: 1,000]
        end
        
        subgraph "Capa RPC"
            SUB[Subscribe Handler<br/>Streaming]
            RES[SendResult Handler]
        end
        
        subgraph "Capa de Estadísticas"
            RESULTS[results: []int]
            CR[clientResults<br/>map[int32][]int]
            CQ[clientQueues<br/>map[int32][]string]
            RM[resultsMu]
            
            RESULTS --> RM
            CR --> RM
            CQ --> RM
        end
        
        subgraph "Capa de Control"
            SP[stopPublishing: bool]
            SPM[stopMu]
            
            SP --> SPM
        end
        
        PUBG --> ROUTER
        ROUTER --> Q1
        ROUTER --> Q2
        ROUTER --> Q3
        
        Q1 --> SUB
        Q2 --> SUB
        Q3 --> SUB
        
        RES --> RESULTS
        RESULTS -.->|>= 1M| SP
    end
    
    subgraph "CLIENTE - Arquitectura Interna"
        subgraph "Configuración"
            CID[Client ID]
            SUBS[Subscriptions<br/>1 o 2 colas]
            PAT[Pattern<br/>fast/normal/slow]
            
            CID --> SUBS
        end
        
        subgraph "Conexión"
            GCONN[gRPC Connection]
            STREAM[Streaming RPC]
            RECONN[Reconnection<br/>Exponential Backoff]
            
            GCONN --> STREAM
            GCONN -.->|fallo| RECONN
        end
        
        subgraph "Loop de Recepción"
            RECV[Receive Message]
            PPROC[Process<br/>según pattern]
            PSEND[Send Result]
            
            RECV --> PPROC
            PPROC --> PSEND
            PSEND --> RECV
        end
        
        subgraph "Límites"
            MAXMSG[Max Messages]
            DUR[Duration]
            STOP[Should Stop]
            
            MAXMSG --> STOP
            DUR --> STOP
        end
        
        SUBS --> STREAM
        STREAM --> RECV
        PSEND -.->|check| STOP
    end
    
    style PUBG fill:#90EE90
    style ROUTER fill:#FFA500
    style Q1 fill:#FFD700
    style Q2 fill:#FFD700
    style Q3 fill:#FFD700
    style RESULTS fill:#DDA0DD
    style SP fill:#FF6B6B
```

---

## 5. Patrones de Comunicación

### Productor-Consumidor: Comunicación Point-to-Point

```mermaid
graph LR
    subgraph "Patrón: Point-to-Point (1:1)"
        P[Productor]
        Q[Cola]
        
        subgraph "Consumidores compiten"
            C1[Cliente 1]
            C2[Cliente 2]
            C3[Cliente 3]
        end
        
        P -->|Trabajo 1| Q
        P -->|Trabajo 2| Q
        P -->|Trabajo 3| Q
        
        Q -->|Solo 1 recibe T1| C1
        Q -->|Solo 1 recibe T2| C2
        Q -->|Solo 1 recibe T3| C3
        
        C1 -.->|Resultado 1| P
        C2 -.->|Resultado 2| P
        C3 -.->|Resultado 3| P
    end
    
    style P fill:#90EE90
    style Q fill:#FFD700
    style C1 fill:#87CEEB
    style C2 fill:#87CEEB
    style C3 fill:#87CEEB
```

**Características**:
- ✅ Consumo competitivo
- ✅ Sin duplicación
- ✅ Balanceo automático
- ❌ Sin flexibilidad de routing

### Publisher-Subscriber: Comunicación Broadcast

```mermaid
graph TB
    subgraph "Patrón: Publish-Subscribe (1:N)"
        PUB[Publisher]
        
        subgraph "Topics"
            T1[Primary Topic]
            T2[Secondary Topic]
            T3[Tertiary Topic]
        end
        
        subgraph "Suscriptores"
            S1[Sub 1<br/>Primary]
            S2[Sub 2<br/>Primary + Secondary]
            S3[Sub 3<br/>Secondary]
            S4[Sub 4<br/>Tertiary]
        end
        
        PUB -->|Msg A| T1
        PUB -->|Msg B| T2
        PUB -->|Msg C| T3
        
        T1 -.->|Broadcast| S1
        T1 -.->|Broadcast| S2
        
        T2 -.->|Broadcast| S2
        T2 -.->|Broadcast| S3
        
        T3 -.->|Broadcast| S4
        
        S1 -.->|Resultado| PUB
        S2 -.->|Resultado| PUB
        S3 -.->|Resultado| PUB
        S4 -.->|Resultado| PUB
    end
    
    style PUB fill:#90EE90
    style T1 fill:#FFD700
    style T2 fill:#FFD700
    style T3 fill:#FFD700
    style S1 fill:#87CEEB
    style S2 fill:#87CEEB
    style S3 fill:#87CEEB
    style S4 fill:#87CEEB
```

**Características**:
- ✅ Multicasting por tema
- ✅ Suscripción flexible
- ✅ Desacoplamiento
- ⚠️ Posible duplicación

---

## 6. Manejo de Errores y Reintentos

### Productor-Consumidor: Estrategia de Errores

```mermaid
graph TB
    START[Cliente hace request] --> TRY[Intentar GetNumbers]
    
    TRY --> SUCCESS{Éxito?}
    
    SUCCESS -->|Sí| PROC[Procesar vector]
    SUCCESS -->|No| ERRCNT[consecutiveFailures++]
    
    ERRCNT --> MAXERR{>= 5 errores?}
    
    MAXERR -->|Sí| STOP[Detener cliente]
    MAXERR -->|No| WAIT1[Sleep 1s]
    
    WAIT1 --> TRY
    
    PROC --> SEND[Enviar resultado]
    
    SEND --> SENDSUC{Éxito?}
    
    SENDSUC -->|Sí| RESET[Reset counter]
    SENDSUC -->|No| ERRCNT2[consecutiveFailures++]
    
    ERRCNT2 --> MAXERR2{>= 5 errores?}
    
    MAXERR2 -->|Sí| STOP
    MAXERR2 -->|No| TRY
    
    RESET --> TRY
    
    STOP --> END[Fin]
    
    style SUCCESS fill:#90EE90
    style SENDSUC fill:#90EE90
    style MAXERR fill:#FF6B6B
    style MAXERR2 fill:#FF6B6B
    style STOP fill:#FF6B6B
```

### Publisher-Subscriber: Estrategia de Reconexión

```mermaid
graph TB
    START[Cliente inicia] --> CONN[Intentar conectar]
    
    CONN --> CONNSUC{Conexión exitosa?}
    
    CONNSUC -->|Sí| SUBS[Subscribe a colas]
    CONNSUC -->|No| BACKOFF[Exponential backoff]
    
    BACKOFF --> ATT[attempts++]
    ATT --> MAXATT{>= 10 intentos?}
    
    MAXATT -->|Sí| FAIL[Fallo fatal]
    MAXATT -->|No| WAIT[Esperar backoff time]
    
    WAIT --> CALCBACK[backoff = min(backoff*2, 30s)]
    CALCBACK --> CONN
    
    SUBS --> RECV[Recibir mensajes]
    
    RECV --> RECVERR{Error stream?}
    
    RECVERR -->|No| PROC[Procesar mensaje]
    RECVERR -->|Sí| LOG[Log error]
    
    PROC --> SEND[Enviar resultado]
    
    SEND --> CHECKERR{Error envío?}
    
    CHECKERR -->|No| RECV
    CHECKERR -->|Sí| LOG2[Log error]
    
    LOG --> RECONN[Intentar reconectar]
    LOG2 --> RECV
    
    RECONN --> CLOSE[Cerrar conexión]
    CLOSE --> CONN
    
    FAIL --> END[Fin con error]
    
    style CONNSUC fill:#90EE90
    style RECVERR fill:#FFD700
    style CHECKERR fill:#FFD700
    style MAXATT fill:#FF6B6B
    style FAIL fill:#FF6B6B
```

---

## 7. Comparación Visual de Garantías

### Productor-Consumidor: Garantías Fuertes

```mermaid
graph LR
    subgraph "GARANTÍAS"
        G1[✓ Exactamente una vez<br/>por trabajo]
        G2[✓ Orden FIFO]
        G3[✓ Sin pérdida de trabajos]
        G4[✓ Sin duplicación]
    end
    
    subgraph "EJEMPLO"
        P[Productor] -->|T1| Q[Cola]
        Q -->|T1 una vez| C1[Cliente 1]
        Q -->|T2 una vez| C2[Cliente 2]
        
        C1 -.->|R1| STATS[Stats]
        C2 -.->|R2| STATS
        
        STATS -->|Total: 2 trabajos<br/>2 resultados| REP[Reporte]
    end
    
    style G1 fill:#90EE90
    style G2 fill:#90EE90
    style G3 fill:#90EE90
    style G4 fill:#90EE90
```

### Publisher-Subscriber: Garantías Flexibles

```mermaid
graph LR
    subgraph "GARANTÍAS"
        G1[⚠️ Potencialmente múltiples<br/>veces por suscriptor]
        G2[⚠️ Orden no garantizado<br/>entre colas]
        G3[⚠️ Mensaje se pierde si<br/>no hay suscriptores]
        G4[✓ Flexibilidad de routing]
    end
    
    subgraph "EJEMPLO"
        P[Publisher] -->|M1| T1[Topic Primary]
        T1 -.->|M1| S1[Sub 1]
        T1 -.->|M1| S2[Sub 2]
        
        S1 -.->|R1| STATS[Stats]
        S2 -.->|R1| STATS
        
        STATS -->|Total: 1 mensaje<br/>2 resultados| REP[Reporte]
    end
    
    style G1 fill:#FFD700
    style G2 fill:#FFD700
    style G3 fill:#FFD700
    style G4 fill:#90EE90
```

---

## 8. Escalabilidad Visual

### Productor-Consumidor: Escalabilidad Horizontal

```mermaid
graph TB
    subgraph "Sistema Base: 3 Clientes"
        P1[Productor<br/>1000 trabajos/s]
        Q1[Cola]
        
        C1A[Cliente 1<br/>333 trabajos/s]
        C1B[Cliente 2<br/>333 trabajos/s]
        C1C[Cliente 3<br/>334 trabajos/s]
        
        P1 --> Q1
        Q1 --> C1A
        Q1 --> C1B
        Q1 --> C1C
    end
    
    subgraph "Escalado: 6 Clientes"
        P2[Productor<br/>2000 trabajos/s]
        Q2[Cola<br/>Buffer aumentado]
        
        C2A[Cliente 1<br/>333 trabajos/s]
        C2B[Cliente 2<br/>333 trabajos/s]
        C2C[Cliente 3<br/>333 trabajos/s]
        C2D[Cliente 4<br/>334 trabajos/s]
        C2E[Cliente 5<br/>333 trabajos/s]
        C2F[Cliente 6<br/>334 trabajos/s]
        
        P2 --> Q2
        Q2 --> C2A
        Q2 --> C2B
        Q2 --> C2C
        Q2 --> C2D
        Q2 --> C2E
        Q2 --> C2F
    end
    
    RES[Resultado: Throughput se duplica<br/>linealmente con clientes]
    
    style P1 fill:#90EE90
    style P2 fill:#90EE90
    style Q1 fill:#FFD700
    style Q2 fill:#FFD700
    style RES fill:#DDA0DD
```

### Publisher-Subscriber: Escalabilidad por Temas

```mermaid
graph TB
    subgraph "Sistema Base: 3 Colas, 3 Suscriptores"
        P1[Publisher<br/>100 msgs/s]
        
        T1A[Primary<br/>50 msgs/s]
        T1B[Secondary<br/>30 msgs/s]
        T1C[Tertiary<br/>20 msgs/s]
        
        S1A[Sub 1<br/>Primary]
        S1B[Sub 2<br/>Secondary]
        S1C[Sub 3<br/>Tertiary]
        
        P1 --> T1A
        P1 --> T1B
        P1 --> T1C
        
        T1A --> S1A
        T1B --> S1B
        T1C --> S1C
    end
    
    subgraph "Escalado: Más Suscriptores"
        P2[Publisher<br/>100 msgs/s]
        
        T2A[Primary<br/>50 msgs/s]
        T2B[Secondary<br/>30 msgs/s]
        T2C[Tertiary<br/>20 msgs/s]
        
        S2A[Sub 1<br/>Primary]
        S2B[Sub 2<br/>Primary]
        S2C[Sub 3<br/>Secondary]
        S2D[Sub 4<br/>Secondary]
        S2E[Sub 5<br/>Tertiary]
        
        P2 --> T2A
        P2 --> T2B
        P2 --> T2C
        
        T2A -.-> S2A
        T2A -.-> S2B
        T2B -.-> S2C
        T2B -.-> S2D
        T2C -.-> S2E
    end
    
    RES[Resultado: Procesamiento paralelo<br/>del mismo mensaje<br/>sin aumentar throughput de entrada]
    
    style P1 fill:#90EE90
    style P2 fill:#90EE90
    style RES fill:#DDA0DD
```

---

## Resumen Visual

```mermaid
graph TB
    subgraph "PRODUCTOR-CONSUMIDOR"
        PC_PROS["VENTAJAS<br/>✓ Simple<br/>✓ Garantías fuertes<br/>✓ Balanceo automático<br/>✓ Alta eficiencia"]
        PC_CONS["DESVENTAJAS<br/>✗ Inflexible<br/>✗ Un solo tipo de trabajo<br/>✗ Acoplamiento fuerte"]
    end
    
    subgraph "PUBLISHER-SUBSCRIBER"
        PS_PROS["VENTAJAS<br/>✓ Flexible<br/>✓ Desacoplamiento<br/>✓ Múltiples procesadores<br/>✓ Extensible"]
        PS_CONS["DESVENTAJAS<br/>✗ Más complejo<br/>✗ Posible duplicación<br/>✗ Gestión de suscripciones"]
    end
    
    DECISION{Tipo de<br/>problema}
    
    DECISION -->|Trabajos únicos| PC_USE["USA<br/>PRODUCTOR-CONSUMIDOR"]
    DECISION -->|Eventos/Notificaciones| PS_USE["USA<br/>PUBLISHER-SUBSCRIBER"]
    
    style PC_PROS fill:#90EE90
    style PS_PROS fill:#90EE90
    style PC_CONS fill:#FFB6B6
    style PS_CONS fill:#FFB6B6
    style PC_USE fill:#87CEEB
    style PS_USE fill:#87CEEB
```

---

**Fecha**: Noviembre 2025  
**Autor**: Análisis basado en implementaciones reales en Go con gRPC
