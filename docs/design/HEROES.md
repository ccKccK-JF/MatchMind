# 英雄目录

第一版英雄目录是静态且纳入版本控制的。每个位置提供两名英雄，不引入实时英雄目录
服务也能完成确定性、位置兼容的选择。

| ID | 名称 | 位置 | 难度 | 输出 | 防御 | 控制 | 团队协作 |
|---|---|---|---:|---:|---:|---:|---:|
| `ironwall` | Ironwall | 先锋 | 35 | 45 | 95 | 80 | 85 |
| `stormguard` | Stormguard | 先锋 | 65 | 65 | 85 | 75 | 80 |
| `shadowstep` | Shadowstep | 游走 | 80 | 80 | 35 | 70 | 75 |
| `pathfinder` | Pathfinder | 游走 | 55 | 60 | 55 | 75 | 90 |
| `starblade` | Starblade | 核心 | 75 | 95 | 40 | 45 | 60 |
| `emberlord` | Emberlord | 核心 | 60 | 90 | 50 | 55 | 65 |
| `windshot` | Windshot | 远程 | 50 | 90 | 30 | 50 | 65 |
| `arcweaver` | Arcweaver | 远程 | 85 | 85 | 35 | 75 | 75 |
| `lifebloom` | Lifebloom | 辅助 | 45 | 30 | 50 | 65 | 98 |
| `chronowarden` | Chronowarden | 辅助 | 80 | 45 | 45 | 95 | 95 |

属性和熟练度范围都是 0 到 100。Worker 为已分配位置选择熟练度最高的兼容英雄；
熟练度相同时按英雄 ID 稳定排序。整个熟练度表为空时，所有兼容英雄使用中性值 50；
只要提交了任意熟练度，未出现的英雄按 0 处理。
