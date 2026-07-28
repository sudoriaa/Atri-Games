import { ArrowLeft } from "lucide-react";
import { Link } from "react-router-dom";

export function NotFoundPage() {
  return <div className="page-wrap not-found"><span>404</span><p className="kicker">LOST BETWEEN WORLDS</p><h1>这个入口似乎不存在。</h1><p>也许它还在制作，或者已经移动到另一条时间线。</p><Link className="button" to="/"><ArrowLeft size={17} /> 返回首页</Link></div>;
}
