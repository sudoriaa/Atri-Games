import { lazy, Suspense } from "react";
import { Route, Routes } from "react-router-dom";
import { SiteLayout } from "./components/SiteLayout";
import { NotFoundPage } from "./pages/NotFoundPage";
import { LoadingState } from "./components/PageState";

const AuthPage = lazy(() => import("./pages/AuthPage").then((module) => ({ default: module.AuthPage })));
const DeveloperPromptPage = lazy(() => import("./pages/DeveloperPromptPage").then((module) => ({ default: module.DeveloperPromptPage })));
const DiscoverPage = lazy(() => import("./pages/DiscoverPage").then((module) => ({ default: module.DiscoverPage })));
const GamePage = lazy(() => import("./pages/GamePage").then((module) => ({ default: module.GamePage })));
const HomePage = lazy(() => import("./pages/HomePage").then((module) => ({ default: module.HomePage })));
const LibraryPage = lazy(() => import("./pages/LibraryPage").then((module) => ({ default: module.LibraryPage })));
const ProfilePage = lazy(() => import("./pages/ProfilePage").then((module) => ({ default: module.ProfilePage })));

export function App() {
  return (
    <Suspense fallback={<LoadingState />}>
      <Routes>
        <Route element={<SiteLayout />}>
          <Route index element={<HomePage />} />
          <Route path="discover" element={<DiscoverPage />} />
          <Route path="games/:slug" element={<GamePage />} />
          <Route path="library" element={<LibraryPage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="developers" element={<DeveloperPromptPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
        <Route path="auth" element={<AuthPage />} />
      </Routes>
    </Suspense>
  );
}
