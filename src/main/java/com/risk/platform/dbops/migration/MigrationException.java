package com.risk.platform.dbops.migration;

/** 迁移相关错误统一异常，code 用于客户端区分失败原因。 */
public class MigrationException extends RuntimeException {

    private final String code;

    public MigrationException(String code, String message) {
        super(message);
        this.code = code;
    }

    public MigrationException(String code, String message, Throwable cause) {
        super(message, cause);
        this.code = code;
    }

    public String code() {
        return code;
    }
}
