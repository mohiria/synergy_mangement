import { useMemo, useState } from "react";
import type { Key, ReactElement } from "react";
import { Transfer, Tree } from "antd";
import type { TransferKey } from "antd/es/transfer/interface";
import type { DataNode } from "antd/es/tree";

// 多选树形穿梭框（AC-53）：左侧按分组树勾选（可整组、可多项），右侧已选列表批量移回。
// 各层复选框统一为 antd 16×16 复选框；搜索、计数与空态由本组件统一处理。
// 组件不含业务规则，只负责选择交互；候选集合与分组由调用方按 API 字段构造。

export type TreeTransferGroup = { key: string; label: string };

export type TreeTransferItem = {
  key: string;
  /** 从根到父的分组路径：任务树为 O → KR，成员树为成员分组一层 */
  groups: TreeTransferGroup[];
  /** 行内容，左树叶子与右侧已选列表共用 */
  label: ReactElement;
  /** 搜索命中文本，调用方预先转小写 */
  search: string;
};

const GROUP_PREFIX = "g:";

type GroupEntry = { node: DataNode; label: string; count: number; depth: number };

function buildTree(items: TreeTransferItem[], unit: string): DataNode[] {
  const roots: DataNode[] = [];
  const groups = new Map<string, GroupEntry>();
  for (const item of items) {
    let siblings = roots;
    let path = "";
    item.groups.forEach((group, depth) => {
      path = path ? `${path}/${group.key}` : group.key;
      let entry = groups.get(path);
      if (!entry) {
        entry = {
          node: { key: `${GROUP_PREFIX}${path}`, title: null, children: [] },
          label: group.label,
          count: 0,
          depth,
        };
        groups.set(path, entry);
        siblings.push(entry.node);
      }
      entry.count += 1;
      siblings = entry.node.children as DataNode[];
    });
    siblings.push({ key: item.key, title: item.label, isLeaf: true });
  }
  // 组内条数要等全部叶子归位后才确定，标题回填放在最后。
  groups.forEach((entry) => {
    entry.node.title = (
      <span className={`tree-transfer-group tree-transfer-group-${entry.depth}`}>
        <b>{entry.label}</b>
        <span>
          {entry.count} {unit}
        </span>
      </span>
    );
  });
  return roots;
}

function collectGroupKeys(nodes: DataNode[], out: string[] = []): string[] {
  for (const node of nodes) {
    if (node.children) {
      out.push(String(node.key));
      collectGroupKeys(node.children, out);
    }
  }
  return out;
}

function TreeBody({
  filteredItems,
  selectedKeys,
  onItemSelectAll,
  searching,
  unit,
}: {
  filteredItems: TreeTransferItem[];
  selectedKeys: TransferKey[];
  onItemSelectAll: (keys: TransferKey[], checkAll: boolean | "replace") => void;
  searching: boolean;
  unit: string;
}) {
  const treeData = useMemo(() => buildTree(filteredItems, unit), [filteredItems, unit]);
  const groupKeys = useMemo(() => collectGroupKeys(treeData), [treeData]);
  const [collapsed, setCollapsed] = useState<string[]>([]);

  if (treeData.length === 0) {
    return <div className="tree-transfer-empty">没有匹配{unit === "人" ? "成员" : "任务"}</div>;
  }

  const visible = new Set(filteredItems.map((item) => item.key));
  const checkedKeys = selectedKeys.map(String).filter((key) => visible.has(key));
  // 搜索时忽略折叠状态，命中项直接展开（与原型 .searching 一致）。
  const expandedKeys = searching ? groupKeys : groupKeys.filter((key) => !collapsed.includes(key));

  return (
    <Tree
      className="tree-transfer-tree"
      blockNode
      checkable
      selectable={false}
      treeData={treeData}
      checkedKeys={checkedKeys}
      expandedKeys={expandedKeys}
      onExpand={(keys) => {
        const open = keys.map(String);
        setCollapsed(groupKeys.filter((key) => !open.includes(key)));
      }}
      onCheck={(checked) => {
        const keys: Key[] = Array.isArray(checked) ? checked : checked.checked;
        const leafKeys = keys.map(String).filter((key) => !key.startsWith(GROUP_PREFIX));
        // 搜索过滤掉的已勾选项保持勾选，避免搜索中途丢失选择。
        const kept = selectedKeys.map(String).filter((key) => !visible.has(key));
        onItemSelectAll([...kept, ...leafKeys], "replace");
      }}
    />
  );
}

export default function TreeTransfer({
  items,
  targetKeys,
  onChange,
  titles,
  unit,
  searchPlaceholder,
  listHeight = 320,
}: {
  items: TreeTransferItem[];
  targetKeys: string[];
  onChange: (keys: string[]) => void;
  titles: [string, string];
  /** 计数单位：任务用「项」，成员用「人」 */
  unit: string;
  searchPlaceholder: string;
  listHeight?: number;
}) {
  const [search, setSearch] = useState("");

  return (
    <Transfer<TreeTransferItem>
      className="tree-transfer"
      dataSource={items}
      targetKeys={targetKeys}
      onChange={(keys) => onChange(keys.map(String))}
      render={(item) => item.label}
      showSearch
      showSelectAll={false}
      onSearch={(direction, value) => {
        if (direction === "left") setSearch(value);
      }}
      filterOption={(input, item) => item.search.includes(input.trim().toLowerCase())}
      titles={titles}
      locale={{ itemUnit: unit, itemsUnit: unit, searchPlaceholder, notFoundContent: `请从左侧选择` }}
      listStyle={{ height: listHeight }}
    >
      {({ direction, filteredItems, selectedKeys, onItemSelectAll }) =>
        direction === "left" ? (
          <TreeBody
            filteredItems={filteredItems}
            selectedKeys={selectedKeys}
            onItemSelectAll={onItemSelectAll}
            searching={search.trim() !== ""}
            unit={unit}
          />
        ) : undefined
      }
    </Transfer>
  );
}
