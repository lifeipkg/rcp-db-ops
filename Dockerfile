# 数据库操作服务（db-ops）
FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /build
COPY pom.xml .
RUN mvn -B -q dependency:go-offline || true
COPY src ./src
RUN mvn -B -q package -DskipTests

FROM eclipse-temurin:17-jre
WORKDIR /app
RUN groupadd -r risk && useradd -r -g risk risk && mkdir -p /app/migrations
COPY --from=build /build/target/db-ops-1.0.0.jar app.jar
USER risk
EXPOSE 8091
ENTRYPOINT ["java", "-XX:MaxRAMPercentage=75.0", "-jar", "app.jar"]
