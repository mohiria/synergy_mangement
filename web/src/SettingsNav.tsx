// 设置页左栏分组分节导航（#216）：项目设置、系统设置、个人中心共用。
// 布局参考 ONES 设置页「分组标题 + 组内条目」的形态，视觉按原型风格基线；空分组不渲染。
export type SettingsNavGroup<K extends string> = {
  title: string;
  items: readonly { key: K; label: string }[];
};

export default function SettingsNav<K extends string>({
  groups,
  active,
  onSelect,
}: {
  groups: readonly SettingsNavGroup<K>[];
  active: K;
  onSelect: (key: K) => void;
}) {
  return (
    <aside className="settings-nav">
      {groups
        .filter((g) => g.items.length > 0)
        .map((g) => (
          <div key={g.title} className="settings-nav-group">
            <div className="settings-nav-group-title">{g.title}</div>
            {g.items.map((it) => (
              <button
                key={it.key}
                type="button"
                className={active === it.key ? "active" : ""}
                onClick={() => onSelect(it.key)}
              >
                {it.label}
              </button>
            ))}
          </div>
        ))}
    </aside>
  );
}
