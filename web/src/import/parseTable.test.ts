import { describe, expect, it } from "vitest";
import * as XLSX from "xlsx";
import { decodeTable, detectDelimiter, parseDelimited, parseWorkbook } from "./parseTable";
import { buildTemplateWorkbook } from "./template";

// 解析层是纯函数（#105）：这里覆盖 #97 收口的 CSV 口径与新加的 xlsx 口径。
// 编码用 Node 的 Buffer 造字节，不依赖浏览器。

const enc = (text: string, encoding: BufferEncoding | "gb18030") =>
  encoding === "gb18030"
    ? // Node 没有 gb18030 编码器，用固定的字节序列表达同一段文本。
      gb18030(text)
    : new Uint8Array(Buffer.from(text, encoding)).buffer;

// 只覆盖测试里用到的字符；够表达「中文不乱码」这件事，不做通用编码器。
const GB = new Map<string, number[]>([
  ["O", [0x4f]],
  [" ", [0x20]],
  [",", [0x2c]],
  ["\r", [0x0d]],
  ["\n", [0x0a]],
  ["标", [0xb1, 0xea]],
  ["题", [0xcc, 0xe2]],
  ["核", [0xba, 0xcb]],
  ["心", [0xd0, 0xc4]],
  ["库", [0xbf, 0xe2]],
]);

function gb18030(text: string): ArrayBuffer {
  const out: number[] = [];
  for (const ch of text) {
    const bytes = GB.get(ch);
    if (!bytes) throw new Error(`测试用 GB18030 表里没有字符 ${ch}`);
    out.push(...bytes);
  }
  return new Uint8Array(out).buffer;
}

describe("decodeTable", () => {
  it("纯 UTF-8 直接读出中文", () => {
    expect(decodeTable(enc("O 标题,核心库", "utf8"))).toBe("O 标题,核心库");
  });

  it("UTF-8 with BOM：剥掉 BOM，首格无残留", () => {
    const buf = new Uint8Array(Buffer.from("﻿O 标题,核心库", "utf8")).buffer;
    expect(decodeTable(buf)).toBe("O 标题,核心库");
  });

  it("GB18030：严格 UTF-8 解不出时回落，中文不乱码", () => {
    expect(decodeTable(enc("O 标题,核心库", "gb18030"))).toBe("O 标题,核心库");
  });
});

describe("detectDelimiter", () => {
  it("按首行判定，正文里偶然出现的制表符不改变口径", () => {
    expect(detectDelimiter("a,b,c\nd\te,f,g")).toBe(",");
  });

  it("粘贴的制表符表格仍走制表符", () => {
    expect(detectDelimiter("a\tb\tc\nd\te\tf")).toBe("\t");
  });

  it("分号分隔的 CSV", () => {
    expect(detectDelimiter("a;b;c\n1;2;3")).toBe(";");
  });

  it("首行在引号里的换行不算行尾", () => {
    expect(detectDelimiter('"a\nb",c,d\n1,2,3')).toBe(",");
  });

  it("单列无分隔符时缺省按逗号", () => {
    expect(detectDelimiter("只有一列\n一行")).toBe(",");
  });
});

describe("parseDelimited", () => {
  it("引号包裹的字段可含分隔符与换行，两个连续引号表示一个引号", () => {
    expect(parseDelimited('a,"b,c",d\n"含\n换行","说""引号""",z')).toEqual([
      ["a", "b,c", "d"],
      ["含\n换行", '说"引号"', "z"],
    ]);
  });

  it("全空行（含只有分隔符的行）被剔除", () => {
    expect(parseDelimited("a,b\n,,\n\nc,d\n")).toEqual([
      ["a", "b"],
      ["c", "d"],
    ]);
  });

  it("CRLF 与 LF 都按行尾处理，单元格两端空白去掉", () => {
    expect(parseDelimited("a , b\r\nc,d")).toEqual([
      ["a", "b"],
      ["c", "d"],
    ]);
  });

  it("可以显式指定分隔符", () => {
    expect(parseDelimited("a\tb", "\t")).toEqual([["a", "b"]]);
  });
});

describe("parseWorkbook", () => {
  const book = (rows: unknown[][]) => {
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet(rows), "Sheet1");
    return XLSX.write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
  };

  it("只读第一张工作表，输出与 CSV 同一种二维数组", () => {
    expect(parseWorkbook(book([["O 标题", "KR 描述"], ["核心库", "兼容性评估"]]))).toEqual([
      ["O 标题", "KR 描述"],
      ["核心库", "兼容性评估"],
    ]);
  });

  it("日期单元格转 YYYY-MM-DD 字符串", () => {
    const rows = parseWorkbook(book([["开始"], [new Date(2026, 2, 9)]]));
    expect(rows[1][0]).toBe("2026-03-09");
  });

  it("全空行被剔除", () => {
    expect(parseWorkbook(book([["a", "b"], ["", ""], ["c", "d"]]))).toEqual([
      ["a", "b"],
      ["c", "d"],
    ]);
  });
});

describe("buildTemplateWorkbook", () => {
  it("表头来自字段定义，第二行是示例", () => {
    const wb = buildTemplateWorkbook([
      { header: "O 标题", sample: "核心库完成国产化替换" },
      { header: "KR 描述" },
    ]);
    const rows = XLSX.utils.sheet_to_json<string[]>(wb.Sheets[wb.SheetNames[0]], {
      header: 1,
      defval: "",
    });
    expect(rows[0]).toEqual(["O 标题", "KR 描述"]);
    expect(rows[1]).toEqual(["核心库完成国产化替换", ""]);
  });
});
