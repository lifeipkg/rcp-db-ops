package com.risk.platform.dbops.web;

import com.risk.platform.dbops.migration.MigrationException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import java.util.Map;

/** 迁移错误翻译成 400 + 编码化原因。 */
@RestControllerAdvice
public class DbOpsExceptionHandler {

    @ExceptionHandler(MigrationException.class)
    public ResponseEntity<Map<String, String>> onMigrationError(MigrationException ex) {
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(Map.of("error", ex.code(), "message", String.valueOf(ex.getMessage())));
    }
}
