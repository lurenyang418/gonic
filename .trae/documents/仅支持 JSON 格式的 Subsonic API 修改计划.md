# 使用事务处理级联删除的实现方案

## 1. 需求分析
将当前依赖数据库外键 `ON DELETE CASCADE` 的级联删除机制，改为通过代码层面的事务控制来实现，确保数据一致性和可维护性。

## 2. 当前删除逻辑分析

### 2.1 现有 DeletePodcast 函数
```go
func (p *Podcasts) DeletePodcast(podcastID int) error {
    // 1. 查找播客
    var podcast db.Podcast
    if err := p.db.Where("id=?", podcastID).First(&podcast).Error; err != nil {
        return err
    }
    // 2. 验证根目录
    if podcast.RootDir == "" {
        return fmt.Errorf("podcast has no root dir")
    }
    // 3. 删除目录
    if err := os.RemoveAll(podcast.RootDir); err != nil {
        return fmt.Errorf("delete podcast directory: %w", err)
    }
    // 4. 删除播客记录（依赖外键级联删除剧集）
    if err := p.db.Where("id=?", podcastID).Delete(db.Podcast{}).Error; err != nil {
        return fmt.Errorf("delete podcast row: %w", err)
    }
    return nil
}
```

### 2.2 现有问题
- 依赖数据库外键约束，数据库配置可能影响功能
- 目录删除与数据库操作不在同一事务中，可能导致数据不一致
- 代码可读性差，级联删除逻辑不明确

## 3. 事务处理方案设计

### 3.1 核心设计原则
- **事务原子性**：所有删除操作要么全部成功，要么全部失败
- **代码显式控制**：明确的级联删除逻辑，不依赖数据库配置
- **数据一致性**：确保文件系统和数据库状态一致

### 3.2 实现步骤
1. **开始事务**
2. **查找播客**
3. **查找所有关联剧集**
4. **删除所有剧集文件**
5. **删除剧集记录**
6. **删除播客目录**
7. **删除播客记录**
8. **提交事务**

### 3.3 代码实现
```go
func (p *Podcasts) DeletePodcast(podcastID int) error {
    return p.db.Transaction(func(tx *db.DB) error {
        // 1. 查找播客
        var podcast db.Podcast
        if err := tx.Where("id=?", podcastID).First(&podcast).Error; err != nil {
            return err
        }
        
        // 2. 验证根目录
        if podcast.RootDir == "" {
            return fmt.Errorf("podcast has no root dir")
        }
        
        // 3. 查找所有关联剧集
        var episodes []db.PodcastEpisode
        if err := tx.Where("podcast_id=?", podcastID).Find(&episodes).Error; err != nil {
            return err
        }
        
        // 4. 删除所有剧集文件（可选，如果目录删除包含这些文件）
        // 注意：如果目录删除包含所有剧集文件，这一步可以省略
        // for _, episode := range episodes {
        //     if episode.Status != db.PodcastEpisodeStatusDeleted {
        //         if err := os.Remove(episode.AbsPath()); err != nil && !os.IsNotExist(err) {
        //             return fmt.Errorf("remove episode file: %w", err)
        //         }
        //     }
        // }
        
        // 5. 删除剧集记录
        if err := tx.Where("podcast_id=?", podcastID).Delete(db.PodcastEpisode{}).Error; err != nil {
            return fmt.Errorf("delete podcast episodes: %w", err)
        }
        
        // 6. 删除播客目录
        if err := os.RemoveAll(podcast.RootDir); err != nil {
            return fmt.Errorf("delete podcast directory: %w", err)
        }
        
        // 7. 删除播客记录
        if err := tx.Where("id=?", podcastID).Delete(db.Podcast{}).Error; err != nil {
            return fmt.Errorf("delete podcast row: %w", err)
        }
        
        return nil
    })
}
```

### 3.4 关键改进
- 使用 `p.db.Transaction` 确保所有操作在同一事务中
- 显式删除剧集记录，不依赖数据库外键
- 目录删除与数据库操作在同一事务上下文
- 代码逻辑更明确，易于维护和调试

## 4. 事务处理的边界情况

### 4.1 目录删除失败
- 如果目录删除失败，事务会自动回滚，数据库记录不会被删除
- 保持数据一致性：要么文件和数据库都删除，要么都不删除

### 4.2 剧集删除失败
- 任何一步删除操作失败，整个事务回滚
- 确保部分删除不会导致数据不一致

### 4.3 并发访问
- 事务隔离级别确保并发删除操作的安全性
- 避免竞态条件导致的数据异常

## 5. 与外键约束的对比

| 特性 | 数据库外键约束 | 代码事务控制 |
|------|----------------|--------------|
| **实现位置** | 数据库层面 | 代码层面 |
| **可读性** | 隐式逻辑，依赖数据库配置 | 显式逻辑，易于理解 |
| **可维护性** | 依赖数据库版本和配置 | 代码可控，易于调试 |
| **性能** | 数据库原生支持，性能较好 | 多次查询，性能略低 |
| **跨数据库兼容性** | 依赖数据库支持 | 不依赖特定数据库特性 |
| **错误处理** | 数据库自动处理 | 代码可自定义错误处理 |

## 6. 建议实施步骤

### 6.1 短期改进
1. 修改 `DeletePodcast` 函数，使用事务处理级联删除
2. 保持外键约束作为安全保障
3. 测试删除功能，确保数据一致性

### 6.2 长期改进
1. 评估其他使用外键约束的地方
2. 逐步将所有级联删除改为事务控制
3. 根据需要考虑移除数据库外键约束
4. 添加完整的单元测试和集成测试

## 7. 预期效果
- 提高代码的可读性和可维护性
- 增强数据一致性保障
- 减少对数据库配置的依赖
- 便于跨数据库迁移
- 更灵活的错误处理机制

## 8. 注意事项

### 8.1 性能考虑
- 对于大量剧集的播客，删除操作可能较慢
- 建议添加日志，便于监控删除操作
- 考虑添加批量删除优化

### 8.2 测试覆盖
- 测试正常删除流程
- 测试删除过程中出错的情况
- 测试并发删除的情况
- 测试删除不存在播客的情况

### 8.3 错误处理
- 确保所有错误都被正确捕获和返回
- 考虑添加更详细的错误信息
- 确保事务回滚机制正常工作

## 9. 总结

使用事务处理级联删除，替代数据库外键约束，能提高代码的可读性、可维护性和跨数据库兼容性，同时确保数据一致性。这种方式更适合现代应用开发，便于调试和扩展。

建议先从 `DeletePodcast` 函数开始实施，验证效果后再逐步推广到其他使用外键约束的场景。