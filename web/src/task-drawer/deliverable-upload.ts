// 一次多文件上传交付物的路由规划（#120）：每个文件建一项（项名取文件名，#113），
// 同名已有项则作为该项的重传，不建第二项。
// deliverableName 与后端 domain.DeliverableName 逐条对齐（裁决 G1）——这里不是复刻业务规则，
// 而是为了把每个文件路由到「建项」还是「重传」，必须与服务端的项名派生结果一致，否则
// 建项请求会被重名 422 挡回。真正的挡重名仍在服务端。

export type UploadPlan = {
  fileName: string;
  action: "create" | "retransmit";
  targetName: string;
};

const NAME_MAX_RUNES = 100;

// deliverableName 项名派生：去掉最后一段扩展名；隐藏文件（首字符是点）整串保留；
// 超过 100 字截断（与服务端一致，保证同名匹配不漂移）。
export function deliverableName(fileName: string): string {
  const file = fileName.trim();
  let name = file;
  const i = file.lastIndexOf(".");
  if (i > 0) name = file.slice(0, i);
  name = name.trim();
  if (name === "") name = file;
  const runes = [...name];
  if (runes.length > NAME_MAX_RUNES) name = runes.slice(0, NAME_MAX_RUNES).join("");
  return name;
}

// planUploads 把一批文件按已有项名分成「建项」与「重传」：匹配不区分大小写（同后端 EqualFold）；
// 同一批内派生出同名的后续文件，作为前面刚建出的那一项的重传。
export function planUploads(fileNames: string[], existingNames: string[]): UploadPlan[] {
  const known = new Map<string, string>();
  for (const n of existingNames) {
    known.set(n.trim().toLowerCase(), n);
  }
  const plans: UploadPlan[] = [];
  for (const fileName of fileNames) {
    const name = deliverableName(fileName);
    const hit = known.get(name.toLowerCase());
    if (hit !== undefined) {
      plans.push({ fileName, action: "retransmit", targetName: hit });
    } else {
      plans.push({ fileName, action: "create", targetName: name });
      known.set(name.toLowerCase(), name);
    }
  }
  return plans;
}
