import * as XLSX from "xlsx";

// 导入器的解析层（#105）：文件或粘贴文本 → 二维单元格数组。
// 纯函数，不认识业务字段，也不碰 DOM——O/KR 导入与任务批量导入共用同一份口径。
// CSV 的编码与分隔符口径来自 #97，本模块只是把它从 ImportModal 里搬出来，不重写。

// CSV 读取（#97）：Excel 另存的 CSV 有三种常见来源——「CSV UTF-8」带 BOM、
// 中文环境默认的「CSV (逗号分隔)」是 GB18030／GBK、以及纯 UTF-8。
// 先剥 BOM，再用严格 UTF-8 试解；解不出（遇到非法字节序列）才回落 GB18030。
export function decodeTable(buffer: ArrayBuffer): string {
  let bytes = new Uint8Array(buffer);
  if (bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) {
    bytes = bytes.subarray(3);
  }
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder("gb18030").decode(bytes);
  }
}

// 分隔符只看首行（表头）：正文单元格里偶然出现的制表符不该改变整篇的切分口径。
// 计数相同时按 制表符 → 逗号 → 分号 取，剪贴板粘贴的表格因此仍走制表符。
const DELIMITERS = ["\t", ",", ";"] as const;

export function detectDelimiter(text: string): string {
  const header = firstLogicalLine(text);
  let best = ",";
  let bestCount = 0;
  for (const d of DELIMITERS) {
    const n = countOutsideQuotes(header, d);
    if (n > bestCount) {
      best = d;
      bestCount = n;
    }
  }
  return best;
}

// 首行要按引号规则取：被引号包裹的字段里可以有换行，那不是行尾。
function firstLogicalLine(text: string): string {
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === '"') {
      if (quoted && text[i + 1] === '"') i++;
      else quoted = !quoted;
    } else if (!quoted && (c === "\n" || c === "\r")) {
      return text.slice(0, i);
    }
  }
  return text;
}

function countOutsideQuotes(line: string, delim: string): number {
  let quoted = false;
  let n = 0;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (c === '"') {
      if (quoted && line[i + 1] === '"') i++;
      else quoted = !quoted;
    } else if (!quoted && c === delim) {
      n++;
    }
  }
  return n;
}

// RFC 4180 口径的切分：双引号包裹的字段里可以有分隔符与换行，两个连续引号表示一个引号。
// 全空行（含 ",,,," 这类只有分隔符的行）在这一步就剔除，不进后面的映射与预览。
export function parseDelimited(text: string, delim = detectDelimiter(text)): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  const endField = () => {
    row.push(field.trim());
    field = "";
  };
  const endRow = () => {
    endField();
    if (row.some((c) => c !== "")) rows.push(row);
    row = [];
  };
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          quoted = false;
        }
      } else {
        field += c;
      }
      continue;
    }
    switch (c) {
      case '"':
        quoted = true;
        break;
      case delim:
        endField();
        break;
      case "\r":
        if (text[i + 1] === "\n") i++;
        endRow();
        break;
      case "\n":
        endRow();
        break;
      default:
        field += c;
    }
  }
  if (field !== "" || row.length > 0) endRow();
  return rows;
}

// xlsx（裁决 B-a ＝ i）：SheetJS 在前端解析，服务端不新增解析端点。
// 只读第一张工作表；合并单元格由 SheetJS 展开后取左上值；日期单元格统一转 YYYY-MM-DD 字符串。
export function parseWorkbook(buffer: ArrayBuffer): string[][] {
  const wb = XLSX.read(buffer, { type: "array", cellDates: true });
  const name = wb.SheetNames[0];
  if (!name) return [];
  const rows = XLSX.utils.sheet_to_json<unknown[]>(wb.Sheets[name], {
    header: 1,
    blankrows: false,
    defval: "",
    raw: true,
  });
  return rows
    .map((row) => row.map(cellText))
    .filter((row) => row.some((c) => c !== ""));
}

function cellText(v: unknown): string {
  if (v == null) return "";
  if (v instanceof Date) {
    // 只取日期部分：导入的开始／截止都是日期，不带时间。
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${v.getFullYear()}-${pad(v.getMonth() + 1)}-${pad(v.getDate())}`;
  }
  return String(v).trim();
}

// 按文件名后缀选解析口径；两条路径都返回同一种二维数组。
export async function parseFile(file: File): Promise<string[][]> {
  const buffer = await file.arrayBuffer();
  if (/\.xlsx?$/i.test(file.name)) return parseWorkbook(buffer);
  return parseDelimited(decodeTable(buffer));
}
