import { describe, expect, it } from "vitest";
import { deliverableName, planUploads } from "./deliverable-upload";

// #120：一次多文件上传交付物——每个文件建一项（项名取文件名，#113），
// 同名已有项则作为该项的重传。项名派生与 domain.DeliverableName 对齐（裁决 G1）。
describe("deliverableName", () => {
  it("去掉最后一段扩展名", () => {
    expect(deliverableName("联调报告.docx")).toBe("联调报告");
    expect(deliverableName("a.b.c.txt")).toBe("a.b.c");
  });
  it("隐藏文件与无扩展名整串保留", () => {
    expect(deliverableName(".gitignore")).toBe(".gitignore");
    expect(deliverableName("无扩展名")).toBe("无扩展名");
  });
  it("首尾空白剔除", () => {
    expect(deliverableName(" 空格.pdf ")).toBe("空格");
  });
  it("超过 100 字截断（与后端一致，保证同名匹配不漂）", () => {
    const long = "字".repeat(120);
    expect(deliverableName(`${long}.docx`)).toBe("字".repeat(100));
  });
});

describe("planUploads", () => {
  it("全新文件逐个建项", () => {
    expect(planUploads(["报告.docx", "清单.xlsx"], [])).toEqual([
      { fileName: "报告.docx", action: "create", targetName: "报告" },
      { fileName: "清单.xlsx", action: "create", targetName: "清单" },
    ]);
  });
  it("与已有项同名走重传，不建第二项", () => {
    expect(planUploads(["报告.pdf"], ["报告"])).toEqual([
      { fileName: "报告.pdf", action: "retransmit", targetName: "报告" },
    ]);
  });
  it("同名匹配不区分大小写（与后端 EqualFold 一致）", () => {
    expect(planUploads(["Report.docx"], ["report"])).toEqual([
      { fileName: "Report.docx", action: "retransmit", targetName: "report" },
    ]);
  });
  it("同批内派生同名：第一个建项，第二个作为该项的重传", () => {
    expect(planUploads(["报告.docx", "报告.pdf"], [])).toEqual([
      { fileName: "报告.docx", action: "create", targetName: "报告" },
      { fileName: "报告.pdf", action: "retransmit", targetName: "报告" },
    ]);
  });
});
