# Hero catalog

MatchMind's first-version Hero catalog is static and version-controlled. Each
role has two Heroes, allowing deterministic role-compatible selection without
adding a catalog service to the real-time matchmaking path.

| ID | Name | Role | Difficulty | Offense | Defense | Control | Teamwork |
|---|---|---|---:|---:|---:|---:|---:|
| `ironwall` | Ironwall | vanguard | 35 | 45 | 95 | 80 | 85 |
| `stormguard` | Stormguard | vanguard | 65 | 65 | 85 | 75 | 80 |
| `shadowstep` | Shadowstep | roamer | 80 | 80 | 35 | 70 | 75 |
| `pathfinder` | Pathfinder | roamer | 55 | 60 | 55 | 75 | 90 |
| `starblade` | Starblade | core | 75 | 95 | 40 | 45 | 60 |
| `emberlord` | Emberlord | core | 60 | 90 | 50 | 55 | 65 |
| `windshot` | Windshot | ranged | 50 | 90 | 30 | 50 | 65 |
| `arcweaver` | Arcweaver | ranged | 85 | 85 | 35 | 75 | 75 |
| `lifebloom` | Lifebloom | support | 45 | 30 | 50 | 65 | 98 |
| `chronowarden` | Chronowarden | support | 80 | 45 | 45 | 95 | 95 |

All attribute and proficiency values use the range 0 to 100. The matchmaking
worker chooses the highest-proficiency Hero compatible with the assigned role;
ties are resolved by Hero ID. If the whole proficiency map is empty, every
compatible Hero receives a neutral score of 50. Once any score is supplied,
an omitted Hero has score 0.
