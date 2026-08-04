import { lazy, Suspense } from "react";
import { Route, Routes } from "react-router-dom";
import { SiteLayout } from "./components/SiteLayout";
import { NotFoundPage } from "./pages/NotFoundPage";
import { LoadingState } from "./components/PageState";

const AuthPage = lazy(() => import("./pages/AuthPage").then((module) => ({ default: module.AuthPage })));
const DeveloperPromptPage = lazy(() => import("./pages/DeveloperPromptPage").then((module) => ({ default: module.DeveloperPromptPage })));
const CreatorPage = lazy(() => import("./pages/CreatorPage").then((module) => ({ default: module.CreatorPage })));
const DiscoverPage = lazy(() => import("./pages/DiscoverPage").then((module) => ({ default: module.DiscoverPage })));
const FeedPage = lazy(() => import("./pages/FeedPage").then((module) => ({ default: module.FeedPage })));
const GamePage = lazy(() => import("./pages/GamePage").then((module) => ({ default: module.GamePage })));
const HomePage = lazy(() => import("./pages/HomePage").then((module) => ({ default: module.HomePage })));
const LibraryPage = lazy(() => import("./pages/LibraryPage").then((module) => ({ default: module.LibraryPage })));
const MyGamesPage = lazy(() => import("./pages/MyGamesPage").then((module) => ({ default: module.MyGamesPage })));
const NotificationsPage = lazy(() => import("./pages/NotificationsPage").then((module) => ({ default: module.NotificationsPage })));
const ProfilePage = lazy(() => import("./pages/ProfilePage").then((module) => ({ default: module.ProfilePage })));
const SafetyPage = lazy(() => import("./pages/SafetyPage").then((module) => ({ default: module.SafetyPage })));
const UploadGamePage = lazy(() => import("./pages/UploadGamePage").then((module) => ({ default: module.UploadGamePage })));

export function App() {
  return (
    <Suspense fallback={<LoadingState />}>
      <Routes>
        <Route element={<SiteLayout />}>
          <Route index element={<HomePage />} />
          <Route path="discover" element={<DiscoverPage />} />
          <Route path="creators/:id" element={<CreatorPage />} />
          <Route path="feed" element={<FeedPage />} />
          <Route path="notifications" element={<NotificationsPage />} />
          <Route path="games/:slug" element={<GamePage />} />
          {/* /games/:slug/play is served as a static file by Caddy; this
              stub prevents React Router from catching the path on client-side
              navigations and rendering a 404 before Caddy can handle it. */}
          <Route path="games/:slug/play/*" element={null} />
          <Route path="library" element={<LibraryPage />} />
          <Route path="my-games" element={<MyGamesPage />} />
          <Route path="upload" element={<UploadGamePage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="safety" element={<SafetyPage />} />
          <Route path="developers" element={<DeveloperPromptPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
        <Route path="auth" element={<AuthPage />} />
      </Routes>
    </Suspense>
  );
}
