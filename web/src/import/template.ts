import * as XLSX from "xlsx";

// 模板生成（#105，裁决 B-b ＝ i）：xlsx 模板由代码现生成并触发下载，
// 表头直接来自调用方的字段定义，与该导入器的字段映射同源——不放静态模板文件，
// 免得字段改了模板还停在旧版。

export type TemplateColumn = {
  /** 表头文案；导入时的表头猜测规则认的就是这一列 */
  header: string;
  /** 示例行的取值，帮使用者看清该列填什么 */
  sample?: string;
};

/** 按字段定义生成一份只含表头与一行示例的工作簿。 */
export function buildTemplateWorkbook(columns: TemplateColumn[], sheetName = "模板"): XLSX.WorkBook {
  const rows = [columns.map((c) => c.header), columns.map((c) => c.sample ?? "")];
  const sheet = XLSX.utils.aoa_to_sheet(rows);
  // 列宽按表头与示例的较长者给一个够读的下限，免得中文表头被挤成两三个字。
  sheet["!cols"] = columns.map((c) => ({
    wch: Math.max(12, c.header.length * 2 + 2, (c.sample ?? "").length + 2),
  }));
  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, sheet, sheetName);
  return wb;
}

/** 生成并下载 xlsx 模板。文件名不带路径，由浏览器落到下载目录。 */
export function downloadTemplate(fileName: string, columns: TemplateColumn[], sheetName?: string) {
  XLSX.writeFile(buildTemplateWorkbook(columns, sheetName), fileName);
}
