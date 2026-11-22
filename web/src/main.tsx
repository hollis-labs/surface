import { app, modules } from "@surface/app"
import { mountPage } from "./boot"
import { galleryViews } from "./gallery"
import "./index.css"

mountPage({ modules, core: galleryViews, app })
