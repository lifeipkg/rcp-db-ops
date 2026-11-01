package com.risk.platform.dbops.web;

import com.risk.platform.dbops.migration.MigrationService;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * 数据库操作入口：POST /dbops/migrate、GET /dbops/versions、POST /dbops/rollback。
 */
@RestController
@RequestMapping("/dbops")
public class DbOpsController {

    private final MigrationService migrationService;

    public DbOpsController(MigrationService migrationService) {
        this.migrationService = migrationService;
    }

    @PostMapping("/migrate")
    public MigrationService.MigrateResult migrate(@RequestBody(required = false) MigrateRequest request) {
        String target = request == null ? null : request.targetVersion();
        return migrationService.migrate(target);
    }

    @GetMapping("/versions")
    public MigrationService.StatusResult versions() {
        return migrationService.status();
    }

    @PostMapping("/rollback")
    public MigrationService.RollbackResult rollback(@RequestBody RollbackRequest request) {
        return migrationService.rollbackTo(request.targetVersion());
    }

    /** targetVersion 缺省表示迁移到最新。 */
    public record MigrateRequest(String targetVersion) {
    }

    /** targetVersion 空串表示全部回滚。 */
    public record RollbackRequest(String targetVersion) {
    }
}
